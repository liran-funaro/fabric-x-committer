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
# SKIP_DEPLOY measures whatever is already deployed, refusing any experiment whose rendered shape does not
# match. For the ageing test that is not an optimisation, it IS the experiment -- and it closes a trap,
# since a missed rung inside a normal ladder triggers a redeploy, which would hand the measurement a fresh
# table and read as "still 14 ms, durable".
runskip() { idle; say "$1 (no deploy -- measuring the table as it stands)"
  if TAG=$1 ONLY=$2 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=2 INV=$3 \
     SKIP_DEPLOY=1 FX_DRAIN_RATE=${4:-20000} ./fx-run-matrix.sh > "/data1/logs/$1.log" 2>&1
  then say "$1 done"
  else say "$1 FAILED rc=$? -- see /data1/logs/$1.log"
  fi; }

run() { idle; say "$1 (drain ${4:-20000}, seed ${5:-480000})"
  if TAG=$1 ONLY=$2 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=4 INV=$3 \
     FX_DRAIN_RATE=${4:-20000} FX_SEED=${5:-480000} ./fx-run-matrix.sh > "/data1/logs/$1.log" 2>&1
  then say "$1 done"
  else say "$1 FAILED rc=$? -- see /data1/logs/$1.log"
  fi; }

# 1. The nosplit batch is running already, under the chain this one replaced, so it is not repeated.
# Its rung 1 has already answered the question it was queued for: 14,354 tps committed at p99 408 ms and a
# 148 ms mean, growth zero, aborts at the configured 4.88%, on a 300 s hold at gap 300,000. The insert cost
# 15.2 ms against 1,709 ms at the 120-way split -- a 112x fall -- while attempts per commit (1.911 against
# 1.91) and width (177.8 against 175 transactions) were IDENTICAL. So the failure path is entered just as
# often and costs two orders of magnitude less: the pre-split controls the fan-out, not the conflict rate.

# 2. Eight tablets, which is no longer the headline. Its six holds read exactly 29,900 ms with means of
# 25-26 s, and 29,900 is an INTERPOLATION inside the 20-30 s bucket rather than a clamp (the histogram's
# top finite bucket, the only censoring value, is 60,000). So those holds really did run at ~30 s and 8
# tablets is a measured failure at 122,000-130,000 tps. Still worth the ladder and the hold, because the
# 252 ms probe at 172,260 is unexplained either way.
# 2. THE BATCH THAT DECIDES THE CONCLUSION, promoted above the eight-tablet anomaly for that reason. The
# 120-way split has never been offered a rate below its own capacity under conflicts: of 70 gap-300,000
# conflict rows at that split, the LOWEST offered rate is 25,000 tps against a ~20,300 capacity, and every
# one of them reports a p99 of 60,000 ms -- the histogram's top bucket, i.e. censored. So "no rate meets the
# bound however low it goes" was an extrapolation from rows that were all past capacity, and these rungs
# (2,500 / 5,000 / 10,000 / 15,000 / 20,000) are the first sub-capacity measurements that layout will ever
# have had. Both outcomes are publishable: a miss at 15,000 from a clean start earns the strong claim
# instead of assuming it, and a pass changes the section's shape -- pre-splitting would then cost capacity
# under conflicts rather than the latency bound, and the negative result would be about throughput alone.
# Ascending, so nothing inherits, and 120 tablets over twelve nodes is 10 per node -- inside splitting's
# high phase at a 10 GiB threshold, ~7.5 MB per tablet per rung -- so the layout cannot drift here.
run ladderlow 9c-ds5-ladderlow $C 2000

# 3. The ceiling of the configuration that just won. Rung 2 of the no-split ladder retired 28,537 tps at
# 192 ms with the busiest host at 3% CPU, so its own rungs will run out while still passing and leave the
# ceiling unbracketed -- which is exactly how split0-ds10 exhausted UP_STEPS and produced a lower bound
# nobody could quote. The conflict-free ceiling on this arm is 518,000, so what this decides is whether the
# 120-way pre-split buys anything at all for this workload: if the conflicting no-split ceiling is anywhere
# near 518,000, it buys nothing on either axis. Layout pinned at the 23 tablets the no-split table settles
# at, with splitting off, so a higher write rate cannot move it mid-ladder.
run nosplithi 9c-nosplit-ds5-hi $C 20000


# 2. The eight-tablet anomaly as a controlled A/B on ONE variable: automatic tablet splitting. It is on by
# default (read from the running master) and it is not hypothetical -- with pre_split_tablets 0 the state
# table went 15 -> 19 -> 23 tablets in two minutes at 15,000 tps while its SST files grew 743 MB -> 1.19 GB.
# Splitting triggers on tablet size in phases set by tablets per NODE: with twelve tablet servers the low
# phase covers up to twelve tablets at a 128 MiB threshold, the high phase up to 288 at 10 GiB. Eight
# tablets is 0.67 per node, so it sits in the low phase -- which is the parsimonious explanation of
# 172,260 tps at 239 ms on a 90 s probe against six 300 s holds at ~30 s: the probe measured 8 tablets and
# the holds measured 8 growing to N. Identical rate lists, so the pair differs in the policy and nothing
# else. If the twin sustains near 172,000, "eight tablets is refuted" becomes "refuted at the DEFAULT
# splitting policy", which is a different claim and one an operator can act on.
#
# The search-based hold8 is dropped: its seed of 200,000 floors at 75,429, and these two ladders bracket
# the same knee with explicit rates and no seed at all.
run hold8nosplit 9c-ds5-hold8-nosplitting $C
run ladder8tab   9c-ds5-ladder8tab         $C

# 4. The tablet axis at a fixed rate, because rate searches cannot answer it: db_insert under conflicts is
# not a service time (2.05 s saturated against 1.33 s draining at the same 96 tablets), and the width moves
# with backlog depth too. 10,000 and 15,000 both clear every capacity here, and a point is only quotable if
# insert x attempts agrees at both -- one rate cannot show it is uncontaminated, two can.
# NEXT, ahead of tabhold and the share sweep, because figure 5 and Table 1 have no current data at all and
# this sweep has been last on every plan for days without completing once. What follows it answers mechanism
# questions the document already records as unsettled and does not depend on. The cost of the order is a
# second arm switch at the end rather than one at the very end -- worth paying, since if anything derails
# overnight the thing lost should be tabhold and not a missing figure.
idle
say "end-to-end size sweep"
TAG=e2esize E2E=1 ONLY=e2e-size REDO=e2e-size300,e2e-size512,e2e-size1024,e2e-size2048,e2e-size3072,e2e-size4096 \
  OUT=/data1/logs/figures-orderer.jsonl SCHEME=ECDSA HOURS=10 \
  INV=/data1/cluster/inventory/cluster-orderer.yaml ./fx-run-matrix.sh > /data1/logs/e2esize.log 2>&1
say "e2e size sweep done"

# The durability question, and the one claim here a reader would act on: does the no-split advantage survive
# the table ageing? Splitting pauses at 12 tablets because that is the low phase's boundary, and the next
# split needs 10 GiB per tablet -- roughly 120 GiB of table. So the layout is stable now and steps ONCE, at
# a size that can be named, after which it climbs toward 288 and may look like the 88/96/120 rows.
#
# Sized from the measured growth rather than guessed: ns_0 grows 0.049 GB per tablet per minute at 56,000
# tps, so 0.875 GB per tablet per million transactions, and the threshold is 9.6 GB per tablet from fresh.
# At 350,000 tps that is ~31 minutes, so the soak is a single 45-minute hold with margin -- FX_HOLD, not a
# ladder. Splitting is deliberately LEFT ON here, unlike ds5-hi: the thing being measured is splitting
# resuming, so pinning it off would make the test impossible by construction. tablets.log records the
# running count every 30 s, so the crossing is observed rather than assumed.
FX_HOLD=2700 run soak 9c-nosplit-ds5-soak $C 20000
# Then the same rate as ds5-hi's first rung, on the soaked deployment, without redeploying. Same rate at two
# table ages, so ageing cannot be confused with rate sensitivity. A redeploy here would hand this a fresh
# table and read as "still 14 ms, durable", which is why it goes through runskip.
runskip ds5age 9c-nosplit-ds5-age $C 20000

# Retargeted to 23/32/48/64: the no-split layout settles at 23 tablets with a 15.2 ms insert while 88 costs
# 1,196 ms, so 3.8x the layout for 79x the cost. A linear per-tablet law under-predicts that by twenty-one,
# which refutes it the way per-key was refuted, and leaves a CLIFF between 23 and 88 that nothing has
# probed. 88/96/120 only re-measure the flat slow side, and ladderlow already covers 120 sub-capacity. 23 is
# the control that separates "23 tablets" from "no SPLIT INTO clause", which the no-split run cannot.
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
say "PLAN -14u-lf COMPLETE"
