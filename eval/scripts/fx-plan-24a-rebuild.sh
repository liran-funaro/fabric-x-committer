#!/usr/bin/env bash
# Session -24's queue, run after the whole fleet was reprovisioned on 2026-09-22 and rebuilt on
# 2026-09-23. Named -24a-rebuild rather than a bare number: two sessions once picked the same number
# and the second overwrote a ten-batch queue.
#
# Order is eval-todo.md's queue, and the one ordering that is NOT negotiable is that the insert_ns
# A/B runs last. Batches 3b, 4, 7 and 8 measure the failure path the rewrite removes, and
# CREATE OR REPLACE is not a live upgrade path, so after the swap they cannot be taken at all.
#
# THE BINARY STAGED BEFORE THIS CHAIN IS THE BASELINE ONE. Verified on the control node:
#   strings out/control-node/bin/Linux/x86_64/committer | grep -c "EXCEPT ALL"       -> 0  (2 if rewrite)
#   strings out/control-node/bin/Linux/x86_64/committer | grep -c unique_violation   -> 2  (1 if rewrite)
# EXCEPT ALL is the present/absent test. unique_violation goes 2 -> 1 and not 2 -> 0, because
# init_database_tmpl.sql carries its own handler. Do NOT discriminate on "ON CONFLICT": it reads 3 in
# both variants, so a rewrite binary passes as "old" and silently voids the baseline.
set -u
cd /data1/scripts || exit 1
say() { echo "### $(date -u +%H:%M:%SZ) $*"; }
idle() { while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done; }

C=/data1/cluster/inventory/cluster.yaml
NS=/data1/cluster/inventory/cluster-nosplitting.yaml
O=/data1/cluster/inventory/cluster-orderer.yaml

run() { idle; say "$1 (drain ${4:-20000}, seed ${5:-480000})"
  if TAG=$1 ONLY=$2 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=4 INV=$3 \
     FX_DRAIN_RATE=${4:-20000} FX_SEED=${5:-480000} ./fx-run-matrix.sh > "/data1/logs/$1.log" 2>&1
  then say "$1 done"
  else say "$1 FAILED rc=$? -- see /data1/logs/$1.log"
  fi; }

runskip() { idle; say "$1 (no deploy -- measuring the table as it stands)"
  if TAG=$1 ONLY=$2 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=2 INV=$3 \
     SKIP_DEPLOY=1 FX_DRAIN_RATE=${4:-20000} ./fx-run-matrix.sh > "/data1/logs/$1.log" 2>&1
  then say "$1 done"
  else say "$1 FAILED rc=$? -- see /data1/logs/$1.log"
  fi; }

# --- batch 3b: does 250,000 hold at twelve tablets? Three separate bring-ups, three readings. ------
# nosplithi read this rate MET (243 ms, insert 17.0 ms) and then MISSED it (29.6 s) after a redeploy.
# Retracted in b054a57f; the document now claims 150,000. Prediction on record before these run
# (cluster-optimization-log.md section 10): a SPLIT outcome, one or two passing, because the rung's
# insert was already 40% above its ladder's flat baseline. 3/3 clean at 12-14 ms refutes that rule.
# Splitting pinned off via $NS: a gflag in exp-vars.yaml is a silent no-op, since a per-experiment
# redeploy never restarts the yb-master.
run nosplit250r1 9c-nosplit250-rep1 $NS 20000
run nosplit250r2 9c-nosplit250-rep2 $NS 20000
run nosplit250r3 9c-nosplit250-rep3 $NS 20000

# --- batch 4: finish the pinned-vs-splitting A/B. -------------------------------------------------
# hold8nosplit is done (ceiling bracketed 150,000-200,000, top rung reproduced on three deployments).
# Its twin is the one whose rows were lost. Note what the lost run established and this one should
# re-check: the table climbs 8 -> 12 four minutes in, BEFORE the first measurement window, so this is
# "12 tablets reached by splitting" against "8 pinned" -- not an 8-tablet ladder.
run ladder8tab 9c-ds5-ladder8tab $C

# --- batch 6: does the no-split advantage survive the table ageing? ------------------------------
# Splitting deliberately LEFT ON ($C): splitting resuming IS the measurement. A single 45-minute hold,
# sized from 0.875 GB per tablet per million transactions against a 9.6 GB threshold.
FX_HOLD=2700 run soak 9c-nosplit-ds5-soak $C 20000
# Same rate on the soaked deployment without redeploying -- a redeploy would hand it a fresh table
# and read as "still 14 ms, durable", which is the trap runskip exists to close.
runskip ds5age 9c-nosplit-ds5-age $C 20000

# --- batch 7: the tablet axis at two fixed rates. ------------------------------------------------
# Fixed rates, not a search: db_insert under conflicts is not a service time (2.05 s saturated
# against 1.33 s draining at the same 96 tablets), and width moves with backlog depth too. A point is
# quotable only if insert x attempts agrees at both rates.
run tabhold 9c-ds5-tabhold $NS 20000

# --- batch 8: the conflict-share sweep, 1% down to 0.001%. ---------------------------------------
# The share is bookkeeping at twelve tablets from 5% to 30%; this asks where the cost appears at all.
# Ids are ANCHORED with $: unanchored, "9c-ds1" also selects "9c-ds10", a share this batch is not about.
run dsshares "9c-ds1$,9c-ds01$,9c-ds001$,9c-ds0001$" $NS 20000

# --- batch 9: nine validator-committers on the nine non-master database nodes. -------------------
run vc9 9c-ds5-vc9 /data1/cluster/inventory/cluster-vc9.yaml 20000

# --- batch 5 (MOVED LAST): the end-to-end size sweep. ---------------------------------------------
# Moved behind every committer-arm batch because assembler3 (10.241.64.25) was down at rebuild time:
# no route to host, no ping, so a machine-level outage rather than sshd. Three assemblers is a
# different configuration from four -- BFT tolerates it, but a size sweep measured on a degraded
# ordering tier is not comparable with the rows it replaces, which is the whole point of re-running it.
# So this gates on the machine rather than assuming it came back.
idle
if timeout 10 ping -c2 -W2 10.241.64.25 >/dev/null 2>&1; then
  say "assembler3 is back -- running the end-to-end size sweep"
  if TAG=e2esize E2E=1 ONLY=e2e-size \
     REDO=e2e-size300,e2e-size512,e2e-size1024,e2e-size2048,e2e-size3072,e2e-size4096 \
     OUT=/data1/logs/figures-orderer.jsonl SCHEME=ECDSA HOURS=10 INV=$O \
     ./fx-run-matrix.sh > /data1/logs/e2esize.log 2>&1
  then say "e2esize done"; else say "e2esize FAILED rc=$? -- see /data1/logs/e2esize.log"; fi
else
  say "SKIPPED e2esize: assembler3 (10.241.64.25) still unreachable. Four assemblers are required;"
  say "  re-run with: TAG=e2esize E2E=1 ONLY=e2e-size REDO=e2e-size300,e2e-size512,e2e-size1024,e2e-size2048,e2e-size3072,e2e-size4096 OUT=/data1/logs/figures-orderer.jsonl HOURS=10 INV=$O ./fx-run-matrix.sh"
fi

say "CHAIN DONE -- batches 3b through 9 complete."
say "The insert_ns A/B (2g) is NOT run here: it needs the rewrite binary staged and re-verified."
