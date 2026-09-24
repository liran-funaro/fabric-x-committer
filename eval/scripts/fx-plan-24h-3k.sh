#!/usr/bin/env bash
# Repeat the 3 KiB point, which is the sweep's one anomaly.
#
# Today's six points give byte throughput 144.1, 211.0, 270.1, 377.9, 352.9, 419.8 MB/s. Every step
# rises except 3 KiB, which falls 7% and then recovers to the highest value of all six at 4 KiB. A dip
# between two higher neighbours is either an outlier or real structure, and one reading cannot say which.
#
# 09-22 measured this point at 135,086 tps -- 415.0 MB/s -- which sits neatly between 2 KiB's 377.9 and
# 4 KiB's 419.8 and makes the curve monotone. Today's 114,872 is 15% lower. So the likeliest reading is
# that today's 3 KiB is the outlier and the lost row was closer to the truth, which a repeat settles.
#
# This is the recorded lesson rather than a new idea: nine mechanisms were once proposed for four
# single-rung anomalies, and the only one that got repeated was settled in twenty-five minutes.
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
say "repeating 3 KiB, the sweep's one non-monotone point"
if TAG=e2esize3k E2E=1 ONLY='e2e-size3072$' REDO=e2e-size3072 \
   OUT=/data1/logs/figures-orderer.jsonl SCHEME=ECDSA HOURS=4 INV=$O \
   ./fx-run-matrix.sh > /data1/logs/e2esize3k.log 2>&1
then say "e2esize3k done"; else say "e2esize3k FAILED rc=$? -- see /data1/logs/e2esize3k.log"; fi
say "3 KiB REPEAT DONE"
