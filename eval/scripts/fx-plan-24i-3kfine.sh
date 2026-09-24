#!/usr/bin/env bash
# 3 KiB at explicit rates, to bracket the ceiling the rate search steps over.
#
# Two independent searches both reported 114,964 for this point, and both got there the same way: every
# rung above it misses on DELIVERY because the arm retires 128,000-132,000 whatever it is offered, and
# the 0.85 step from 135,252 lands on 114,964 without trying anything in between. So the value is
# reproducible and still about 12% below what the arm sustains.
#
# That is the sweep's one non-monotone point. 114,872 is 352.9 MB/s, a dip between 2 KiB's 377.9 and
# 4 KiB's 419.8; 128,000-131,000 would be 394-403 MB/s, which is monotone. If one of these three holds,
# the dip was the ladder rather than the pipeline.
set -u
cd /data1/scripts || exit 1
say() { echo "### $(date -u +%H:%M:%SZ) $*"; }
idle() { while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "[f]x-run-matrix" >/dev/null \
                || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done; }

O=/data1/cluster/inventory/cluster-orderer.yaml

idle
for h in $(seq 23 42); do
  timeout 5 ping -c1 -W2 10.241.64.$h >/dev/null 2>&1 || { say "!! 10.241.64.$h unreachable; aborting"; exit 1; }
done
say "3 KiB at 120k, 126k and 132k offered, each held rather than searched"
if TAG=e2e3kfine E2E=1 ONLY=e2e-size3072-fine \
   OUT=/data1/logs/figures-orderer.jsonl SCHEME=ECDSA HOURS=6 INV=$O \
   ./fx-run-matrix.sh > /data1/logs/e2e3kfine.log 2>&1
then say "e2e3kfine done"; else say "e2e3kfine FAILED rc=$? -- see /data1/logs/e2e3kfine.log"; fi
say "3 KiB FINE SWEEP DONE"
