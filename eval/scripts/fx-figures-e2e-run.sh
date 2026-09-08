#!/usr/bin/env bash
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
# Switch the cluster to the real-orderer arm and measure the same figures end to end. Runs ON the
# monitor, detached: the whole sequence is one process so that losing the controlling session does
# not leave a half-switched cluster.
#
# The counterpart of fx-figures-run.sh, which switches the other way. Every figure this evaluation
# has published so far measures the committer with a mock orderer inside the load generator, which
# is what the paper's Section 6.3 does. This arm puts a real Arma ordering service in the path --
# 4 parties, 2 shards, one component per machine across 20 machines -- so the numbers include
# ordering. The paper has no end-to-end figure; it publishes ordering alone (414,000 tps at this
# topology) and the committer alone, and those two bracket what this should find.
set -u -o pipefail

source /data1/cluster/bin/fx-env.sh
cd "$FX_PROJECT" || exit 1

ORDERER=/data1/cluster/inventory/cluster-orderer.yaml
COMMITTER=/data1/cluster/inventory/cluster.yaml
LOG=/data1/logs/figures-orderer.log

say() { echo "=== $(date +%H:%M:%S) $*"; }

say "tearing down the committer-only arm"
ANSIBLE_INVENTORY=$COMMITTER make teardown || echo "!! teardown returned $?, continuing"

# Teardown drops the Fabric CA's database, but the admin's enrolled MSP is on disk in the CA's
# deploy directory and outlives it, so the next enrollment presents a certificate the fresh
# registry has never seen and crypto generation stops at "Authentication failure". The CA's own
# state has to go with its database.
say "wiping the Fabric CA's stale enrollment state"
ANSIBLE_INVENTORY=$ORDERER make hard-wipe TARGET_HOSTS=fabric_cas || echo "!! CA wipe returned $?"

# committer_build_bin is false, so the committer and loadgen binaries are built on the workstation
# and staged into the collection's out/ tree; a setup run empties that tree before the transfer play
# reads from it, and then fails every host with "could not find ... on the Ansible Controller".
say "restoring the staged committer binaries"
install -m 0750 -D /data1/bin-stage/committer /data1/bin-stage/loadgen \
  -t "$FX_PROJECT/out/control-node/bin/Linux/x86_64/" || echo "!! staging restore returned $?"

say "setting up the real-orderer arm (binaries, crypto, genesis, configs)"
ANSIBLE_INVENTORY=$ORDERER make setup || { echo "!! setup failed"; exit 1; }

say "starting it"
ANSIBLE_INVENTORY=$ORDERER make start || { echo "!! start failed"; exit 1; }

# The smoke check, in the order a failure would appear. This arm has only ever carried a 2,000 tps
# soak, and the one thing that would break silently is the ordering service producing blocks the
# committer will not accept -- which shows up as a sidecar height that never advances while every
# process is up and healthy.
say "smoke: waiting for the sidecar's block height to advance"
height() {
  curl -sk --max-time 10 -G "https://localhost:9090/api/v1/query" \
    --data-urlencode 'query=max(sidecar_ledger_block_height)' |
    python3 -c 'import json,sys; r=json.load(sys.stdin)["data"]["result"]; print(int(float(r[0]["value"][1])) if r else 0)' 2>/dev/null || echo 0
}
first=$(height)
for _ in $(seq 1 30); do
  sleep 10
  now=$(height)
  if [ "${now:-0}" -gt "${first:-0}" ]; then
    say "smoke: height advanced $first -> $now"
    break
  fi
done
if [ "${now:-0}" -le "${first:-0}" ]; then
  echo "!! the sidecar's height did not advance in 300 s; not starting the measurement"
  echo "!! check an assembler's log and the sidecar's, in that order"
  exit 1
fi

say "running the end-to-end figures"
# The ladder first: this arm's ceiling has never been measured, so the panels have nothing to seed
# from until it reports. The panel run is a second invocation with FX_SEED set from its answer.
exec env FX_MATRIX=e2e \
  FX_INVENTORY=$ORDERER \
  FX_OUT=/data1/logs/figures-orderer.jsonl \
  FX_DEPLOY_PLAN=setup \
  FX_DRAIN_RATE=2000 \
  /data1/cluster/bin/fx-figures.py "${1:-e2e-curve}" >>"$LOG" 2>&1
