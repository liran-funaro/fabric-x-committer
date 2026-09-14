#!/usr/bin/env bash
# Session -14's queue, ordered by what the document still needs rather than by what is interesting.
#
# Named -lf rather than -14q because two sessions independently wrote different fx-plan-14p.sh files
# today and the second overwrote the first, which killed a ten-batch queue and left the cluster running
# one batch. A number is not a name when more than one session is picking numbers.
set -u
cd /data1/logs || exit 1
say() { echo "### $(date +%H:%M:%S) $*"; }
idle() { while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done; }
C=/data1/cluster/inventory/cluster.yaml
# 4th arg: drain rate. 5th: search seed -- and a seed can only probe down to seed x 0.85^6, so a seed
# carried over from a conflict-free shape cannot reach a conflict knee and the batch reports "no rate met"
# indistinguishably from there being no usable rate. Fixed-rate ladders take no seed at all, which is one
# of the reasons most of this queue is now ladders.
run() { idle; say "$1 (drain ${4:-20000}, seed ${5:-480000})"
  if TAG=$1 ONLY=$2 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=4 INV=$3 \
     FX_DRAIN_RATE=${4:-20000} FX_SEED=${5:-480000} ./fx-run-matrix.sh > "/data1/logs/$1.log" 2>&1
  then say "$1 done"
  else say "$1 FAILED rc=$? -- see /data1/logs/$1.log"
  fi; }

# 1. The section's only positive result, and it is unconfirmed for a fixable reason. With pre-splitting
# DISABLED, split0-ds10 climbed 17 rungs to 102,727 offered at inflight_growth 0 and a p99 pinned at
# 195-197 ms, committing 92,958; split0-ds30 reached 76,352 at 192-195 ms. Both ran out of UP_STEPS while
# still passing, so neither found a ceiling. No hold confirms it because the driver holds at the last
# PASSING probe, which was the top of the climb: hold 1 failed there and holds 2 and 3 inherited its
# backlog, reading finished above offered. These are ascending fixed-rate ladders starting well below the
# known-passing probe, so rung 1 is the confirmation and the rungs above hunt the ceiling.
run nosplit 9c-nosplit-ds $C 5000

# 2. Eight tablets, which is no longer the headline. Its six holds read exactly 29,900 ms with means of
# 25-26 s, and 29,900 is an INTERPOLATION inside the 20-30 s bucket rather than a clamp (the histogram's
# top finite bucket, the only censoring value, is 60,000). So those holds really did run at ~30 s and 8
# tablets is a measured failure at 122,000-130,000 tps. Still worth the ladder and the hold, because the
# 252 ms probe at 172,260 is unexplained either way.
run ladder8tab 9c-ds5-ladder8tab $C
run hold8      9c-ds5-hold8      $C 20000 200000

# 3. The only uncontaminated latency for the 120-way split: every rung below its ~20,235 capacity.
# Ascending, so nothing inherits. Now also the test of whether attempts-per-commit stays near 1.79 as the
# rate falls -- if it does, the 2.3-3.4 s service time is the whole story at this layout.
run ladderlow 9c-ds5-ladderlow $C 2000

# 4. The tablet axis at a fixed rate, because rate searches cannot answer it: db_insert under conflicts is
# not a service time (2.05 s saturated against 1.33 s draining at the same 96 tablets), and the width moves
# with backlog depth too. 10,000 and 15,000 both clear every capacity here, and a point is only quotable if
# insert x attempts agrees at both -- one rate cannot show it is uncontaminated, two can.
run tabhold 9c-ds5-tabhold $C 2000

# 5. Conflict share at fixed layout: the cleanest lever on attempts-per-commit. 0.01% and 0.001% are here
# because 1% and 0.1% are both predicted misses -- at width ~300 keys the share of BATCHES holding a
# conflict is 1-(1-p)^150, and a 99th percentile absorbs 1% of transactions and no more.
run ds1    9c-ds1    $C 2000 30000
run ds01   9c-ds01   $C 5000 30000
run ds001  9c-ds001  $C 5000
run ds0001 9c-ds0001 $C 20000

# 6. The published tier width: nine validator-committers on the nine database nodes carrying no master.
run vc9 9c-ds5-vc9 /data1/cluster/inventory/cluster-vc9.yaml 2000 30000

# 7. Figure 5 and Table 1, never completed, and the only end-to-end work outstanding. Own arm, so it goes
# last: switching arms is a full bring-up and there is no way back inside a batch.
idle
say "end-to-end size sweep"
TAG=e2esize E2E=1 ONLY=e2e-size REDO=e2e-size300,e2e-size512,e2e-size1024,e2e-size2048,e2e-size3072,e2e-size4096 \
  OUT=/data1/logs/figures-orderer.jsonl SCHEME=ECDSA HOURS=10 \
  INV=/data1/cluster/inventory/cluster-orderer.yaml ./fx-run-matrix.sh > /data1/logs/e2esize.log 2>&1
say "PLAN -14q-lf COMPLETE"
