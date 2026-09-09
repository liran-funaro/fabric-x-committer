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
# ordering. It reports one figure, the latency-throughput curve. The paper has no end-to-end figure;
# it publishes ordering alone (414,000 tps at this topology) and the committer alone, and those two
# bracket what this should find.
set -u -o pipefail

source /data1/cluster/bin/fx-env.sh
cd "$FX_PROJECT" || exit 1

ORDERER=/data1/cluster/inventory/cluster-orderer.yaml
COMMITTER=/data1/cluster/inventory/cluster.yaml
LOG=/data1/logs/figures-orderer.log

say() { echo "=== $(date +%H:%M:%S) $*"; }

# Hard-wipe every host, not teardown, and not a CA-only wipe. Five bring-up attempts established why,
# and each of the two faults below produces the SAME assembler panic -- which is why fixing one and
# retrying looks like no progress:
#
#  * `make teardown` does not remove a host's MSP. The committer sidecar's certificate from an earlier
#    generation survived every teardown; `make setup` fetches each host's existing MSP into the org tree,
#    so that stale leaf landed in org1's msp/knowncerts, the genesis block embedded it, and the assembler
#    rejected the whole bundle. Fabric validates every certificate in an MSP, so one stale leaf is fatal.
#  * `make hard-wipe TARGET_HOSTS=fabric_cas` re-initialises the CA's key. Wiping only the CA while hosts
#    keep identities enrolled under the old key guarantees that mismatch: a cacert and a leaf minutes
#    apart that cannot verify each other.
#  * Teardown without a CA wipe fails a third way -- it clears the CA's registry, which lives on its own
#    database host, while the admin MSP on the CA host survives, giving "Code:20 Authentication failure".
#
# Wiping everything at once leaves one fresh CA key, one fresh registry, and no host holding an older
# identity. It also discards the database and the ledgers, which a measurement wants anyway.
say "hard-wiping every host: MSPs, CA registry, CA key, database, ledgers"
ANSIBLE_INVENTORY=$ORDERER make hard-wipe TARGET_HOSTS=all || echo "!! hard-wipe returned $?, continuing"

say "clearing the control node's fetch and config trees"
rm -rf "$FX_PROJECT/out/control-node/fetched" "$FX_PROJECT/out/control-node/config"

# committer_build_bin is false, so the committer and loadgen binaries are built on the workstation and
# staged into the collection's out/ tree; a setup run empties that tree before the transfer play reads
# from it, and then fails every host with "could not find ... on the Ansible Controller".
say "restoring the staged committer binaries"
install -m 0750 -D /data1/bin-stage/committer /data1/bin-stage/loadgen \
  -t "$FX_PROJECT/out/control-node/bin/Linux/x86_64/" || echo "!! staging restore returned $?"

say "setting up the real-orderer arm (binaries, crypto, genesis, configs)"
ANSIBLE_INVENTORY=$ORDERER make setup || { echo "!! setup failed"; exit 1; }

# Gate on the crypto before deploying it. Every certificate in every org tree, not a sample: the fault
# this catches is one bad member of a set, and an earlier version of this check sampled with
# `find ... | head -1`, happened to pick a freshly enrolled cert, passed, and let a deployment proceed
# that could not start. Verifying the wrong artifact is not verification.
say "gate: every certificate in every org MSP must verify against its own CA"
bad=0
for D in "$FX_PROJECT"/out/control-node/fetched/crypto/peerOrganizations/* \
         "$FX_PROJECT"/out/control-node/fetched/crypto/ordererOrganizations/*; do
  [ -d "$D" ] || continue
  CA=$(ls "$D"/msp/cacerts/*.pem 2>/dev/null | head -1)
  [ -n "$CA" ] || continue
  while read -r c; do
    [ -n "$c" ] || continue
    openssl verify -CAfile "$CA" "$c" >/dev/null 2>&1 || {
      echo "!! does not verify: $c"
      bad=$((bad + 1))
    }
  done < <(find "$D" -name '*.pem' | grep -vE 'cacerts|tlscacerts|keystore|tls/')
done
[ "$bad" -gt 0 ] && { echo "!! $bad certificate(s) do not verify; not starting"; exit 1; }
say "all org certificates verify"

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

say "running the end-to-end ladder"
# One ladder, one figure: what the whole pipeline costs at a given rate. No knee searches -- there are
# no bar panels on this arm, because the paper has no end-to-end figure for them to sit beside.
exec env FX_MATRIX=e2e \
  FX_INVENTORY=$ORDERER \
  FX_OUT=/data1/logs/figures-orderer.jsonl \
  FX_DEPLOY_PLAN=setup \
  FX_DRAIN_RATE=2000 \
  /data1/cluster/bin/fx-figures.py "${1:-e2e-curve}" >>"$LOG" 2>&1
