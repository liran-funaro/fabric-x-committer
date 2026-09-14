#!/usr/bin/env bash
# The tablet layout, which the single-variable A/B says is the knob, before the long sweep.
#
# Waits for plan8 to be gone as well as for the cluster to be idle: two drivers on one cluster
# corrupt both runs, and the runner's guard refuses the second rather than queueing it, so a
# batch that starts too early is a batch that is silently lost.
set -u
cd /data1/logs || exit 1
say() { echo "### $(date +%H:%M:%S) $*"; }
idle() {
  while pgrep -f "[f]x-plan8" >/dev/null \
     || pgrep -f "[f]x-figures.py" >/dev/null \
     || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done
}

run() { # tag  only  inventory  hours
  idle
  say "$1"
  TAG=$1 ONLY=$2 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=$4 INV=$3 \
    ./fx-run-matrix.sh > "/data1/logs/$1.log" 2>&1
  say "$1 done: $(grep -c 'curve limit\|hold limit' "/data1/logs/$1-driver.log" 2>/dev/null || echo 0) measurements"
}

C=/data1/cluster/inventory/cluster.yaml
run split8ladder 9c-ds5-split8-ladder $C 4
run split32      9c-ds5-split32       $C 2
run vc9          9c-ds5-vc9           /data1/cluster/inventory/cluster-vc9.yaml 3

idle
say "end-to-end size sweep"
TAG=e2esize E2E=1 ONLY=e2e-size REDO=e2e-size300,e2e-size1024,e2e-size4096 \
  OUT=/data1/logs/figures-orderer.jsonl SCHEME=ECDSA HOURS=10 \
  INV=/data1/cluster/inventory/cluster-orderer.yaml ./fx-run-matrix.sh > /data1/logs/e2esize.log 2>&1
say "PLAN 9 COMPLETE"
