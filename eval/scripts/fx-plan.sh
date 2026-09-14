#!/usr/bin/env bash
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
# Run the remaining evaluation batches back to back, so the cluster does not idle between them.
#
# Each batch is one fx-run-matrix.sh invocation, which brings its arm up from stop-wipe-setup-start
# before measuring -- that is the cleanup between experiments, and it is why the batches can be chained
# without anything in between. A batch that fails does not stop the chain: the arms are independent and
# a failed one is better diagnosed from its own log than by holding the cluster idle.
#
# Waits for any driver already running rather than refusing, so this can be launched while the previous
# batch is still going.
set -u
cd "$(dirname "$0")" || exit 1
LOGS=${LOGS:-/data1/logs}
COMMITTER=/data1/cluster/inventory/cluster.yaml
ORDERER=/data1/cluster/inventory/cluster-orderer.yaml
say() { echo "### $(date +%H:%M:%S) $*"; }

say "waiting for any driver already running"
while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 60; done
say "cluster is free"

# 1. Why the conflict workload collapses: three variants of one 5% point. Fewer tablets and a narrower
#    graph chunk both stay under YugabyteDB's multi-key read-batching cliff, for different reasons; the
#    global graph manager tests the alternative explanation. Run before the 9c series, because if the
#    cliff is the mechanism then the series should be measured with whichever variant clears it.
say "batch 1: conflict diagnosis"
TAG=conflict-why ONLY=9c-ds5- OUT=$LOGS/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=4 INV=$COMMITTER \
  ./fx-run-matrix.sh > "$LOGS/conflict-why.log" 2>&1
say "batch 1 done: $(grep -c 'hold limit' "$LOGS/conflict-why-driver.log" 2>/dev/null || echo 0) holds"

# 2. The double-spend series itself, at the derived reference gap. REDO because the 5% and 10% points
#    already carry rows from the gap that measured a dependency convoy.
say "batch 2: double-spend series"
TAG=ds-series ONLY=9c-ds REDO=9c-ds5,9c-ds10,9c-ds20,9c-ds30 OUT=$LOGS/figures-ecdsa.jsonl \
  SCHEME=ECDSA HOURS=5 INV=$COMMITTER ./fx-run-matrix.sh > "$LOGS/ds-series.log" 2>&1
say "batch 2 done"

# 3. The end-to-end size sweep: 300 B re-measured now the first probe is drained into, 3 KiB added, and
#    holds attempted at every size -- the driver redeploys before a hold and refuses one that would not
#    fit the volume, which is what the earlier probe-only points predate.
say "batch 3: end-to-end size sweep"
TAG=e2e-size E2E=1 ONLY=e2e-size REDO=e2e-size300,e2e-size1024,e2e-size4096 \
  OUT=$LOGS/figures-orderer.jsonl SCHEME=ECDSA HOURS=8 INV=$ORDERER \
  ./fx-run-matrix.sh > "$LOGS/e2e-size.log" 2>&1
say "batch 3 done"

say "PLAN COMPLETE"
