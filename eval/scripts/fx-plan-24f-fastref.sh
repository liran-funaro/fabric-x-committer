#!/usr/bin/env bash
# The fast half of the queue-metric pair, on demand rather than by coin flip.
#
# Everything the database DOES is now equal between the regimes: intents-DB seeks per committed
# transaction are 15.0-16.9 across both regimes and both table histories, stalls are zero, and read
# bandwidth tracks throughput. So the 24x insert latency is queueing, and fx-graph-sampler.py now
# samples the queues. What is missing is a FAST window with those metrics: 250,000 lands fast about a
# third of the time, and rep1 and rep2 both landed slow.
#
# 150,000 at the same twelve-tablet pre-split is reliably fast -- ladder8tab met it twice and the
# 250,000 fast regime retires more than that -- so this is the same configuration one rate lower.
set -u
cd /data1/scripts || exit 1
say() { echo "### $(date -u +%H:%M:%SZ) $*"; }
idle() { while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "[f]x-run-matrix" >/dev/null \
                || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done; }

NS=/data1/cluster/inventory/cluster-nosplitting.yaml

idle
for s in fx-intents-sampler fx-graph-sampler; do
  pgrep -f "[${s:0:1}]${s:1}" >/dev/null || say "!! $s is not running; the pair needs it"
done
say "150k fast reference at twelve tablets, with queue and intents sampling"
if TAG=fastref ONLY='9c-nosplit150-fast$' OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=2 \
   INV=$NS FX_DRAIN_RATE=20000 ./fx-run-matrix.sh > /data1/logs/fastref.log 2>&1
then say "fastref done"; else say "fastref FAILED rc=$? -- see /data1/logs/fastref.log"; fi
say "FAST REFERENCE DONE"
