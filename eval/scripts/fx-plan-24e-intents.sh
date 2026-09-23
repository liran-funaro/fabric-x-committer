#!/usr/bin/env bash
# The 250,000 repeats again, with the intents-DB sampler running: the fast/slow comparison for
# the only mechanism still standing.
#
# Everything on the committer's side is now refuted -- read validation is 1-5 ms in every
# condition, the retry path is never entered, YugabyteDB reports no write-write conflicts, the
# graph's cap is not the lever. What is left is the write path, and the two regimes differ by two
# orders of magnitude of in-flight transactions, hence of provisional records in the intents DB.
#
# fx-intents-sampler.py polls all twelve tablet servers directly, because Prometheus does not
# scrape intentsdb_* at all: the tserver exposes 682 metric names on :5340 and the collection's
# scrape config keeps a far narrower set. First fast-regime readings, for reference: no RocksDB
# stalls on either DB, ~2.0 billion intents-DB seeks on ns_0, and 380 GB read against 567 MB
# written -- a 670:1 ratio that is the per-write intent check.
#
# REDO because 3b already left confirmed rows for these three ids; without it the driver skips
# them and the batch reports success having measured nothing.
#
# Three reps because the outcome is a coin flip: 3 of 6 readings met this rate. One rep can easily
# give two fast or two slow and answer nothing.
set -u
cd /data1/scripts || exit 1
say() { echo "### $(date -u +%H:%M:%SZ) $*"; }
idle() { while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "[f]x-run-matrix" >/dev/null \
                || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done; }

NS=/data1/cluster/inventory/cluster-nosplitting.yaml

idle
if ! pgrep -f "[f]x-intents-sampler" >/dev/null; then
  say "!! the intents sampler is not running; starting it, since the batch is pointless without it"
  nohup python3 /data1/scripts/fx-intents-sampler.py >> /data1/logs/intents-sampler.log 2>&1 &
  sleep 5
fi
EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
if [ "$EA" != "0" ]; then say "!! staged binary is not the baseline (EXCEPT ALL=$EA); aborting"; exit 1; fi

say "250k reps with intents sampling (baseline binary, 12 tablets, splitting pinned off)"
if TAG=intentreps ONLY="9c-nosplit250-rep1$,9c-nosplit250-rep2$,9c-nosplit250-rep3$" \
   REDO=9c-nosplit250-rep1,9c-nosplit250-rep2,9c-nosplit250-rep3 \
   OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=4 INV=$NS FX_DRAIN_RATE=20000 \
   ./fx-run-matrix.sh > /data1/logs/intentreps.log 2>&1
then say "intentreps done"; else say "intentreps FAILED rc=$? -- see /data1/logs/intentreps.log"; fi
say "INTENTS COMPARISON DONE"
