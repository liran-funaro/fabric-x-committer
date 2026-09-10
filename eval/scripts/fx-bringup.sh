#!/usr/bin/env bash
# Bring up a cluster arm in the only order that works. INV selects the arm; everything else is the
# same sequence for both, and the one step that is not (`make init`) is gated on the inventory:
#
#   INV=inventory/cluster.yaml                 the committer-only arm, mock orderer in the generator
#   INV=inventory/cluster-orderer.yaml         the end-to-end arm, four shards
#   INV=inventory/cluster-orderer-8shard.yaml  the end-to-end arm, eight shards
#
# The order:
#
#   stop -> wipe -> setup -> gate on crypto -> start -> init -> gate on a committed rate
#
# Every element of that order was paid for. `make start` is not a repair operation: it starts what is
# not running and leaves anything already up untouched, so wipe-then-setup-then-start leaves live
# processes holding pre-wipe state. That produced three distinct faults in one day -- yb-master
# holding certificates from before the crypto was reissued, so Raft never formed; the Fabric CA
# serving a registry older than its own key; and Prometheus running a container spec asking for a
# web-config.yaml the collection no longer renders, which showed as an empty Grafana.
#
# Two faults made this arm produce no valid measurement at all for about a dozen bring-ups, and both
# present identically: the pipeline runs, blocks flow, height climbs, and 100% of transactions come
# back ABORTED_SIGNATURE_INVALID.
#
#   * THE WIPE WAS NOT WIPING THE DATABASE. `make hard-wipe` clears /data1/fabric-x. YugabyteDB's data
#     directories are /data1/yb-master and /data1/yb-tserver,/data2/yb-tserver -- SIBLINGS of that
#     path, not children of it. Read them off the running process; do not infer them from the deploy
#     directory. A database that survives a wipe keeps a namespace policy signed by a retired key:
#     880,706 ABORTED_SIGNATURE_INVALID against exactly 1 COMMITTED, with the pipeline healthy,
#     because it was. It also hangs the database's own init script -- `create database yugabyte` sat
#     22 minutes in RPCWait/CatalogRead rather than erroring "already exists". Deleting only
#     /data2/yb-tserver is WORSE than deleting neither: one live data directory per tserver and one
#     destroyed is corrupt rather than stale. Hence the gate below.
#   * THE NAMESPACE HAS TO BE CREATED BY `make init`. loadgen_generate_namespace is false on this arm
#     because a namespace-creation TX endorses against an MSP rule the load generator has no material
#     for. Without init, namespace 0 has no policy and every signature is rejected.
#
# The CA lives on the control node, which the wipe play cannot clean, and its pgdata needs
# `podman unshare rm -rf`. It is removed BEFORE hard-wipe, or the wipe play dies on its permissions
# and masks whatever failed after it.
#
# Every gate here checks the artifact, not an exit code: every certificate in every org tree (an
# earlier version sampled one with `find | head -1`, passed, and let a deployment start that could
# not), no database state on any host, and a non-zero committed rate with a present latency series.
# Block height is not a gate -- a 100% abort run satisfies it happily.
#
# Every path here is on the control node, which is where this runs: it is one detached process so that
# losing the controlling session cannot leave a half-switched cluster.
set -u -o pipefail
source /data1/cluster/bin/fx-env.sh
cd "$FX_PROJECT" || exit 1
INV=${INV:-/data1/cluster/inventory/cluster-orderer.yaml}
ANS="$FX_PROJECT/.venv/bin/ansible"
say() { echo "=== $(date +%H:%M:%S) $*"; }

if pgrep -f "[a]nsible-playbook" >/dev/null; then echo "!! a play is already running; refusing"; exit 1; fi
say "inventory: $INV"

say "STOP everything first, so nothing survives holding pre-wipe state"
ANSIBLE_INVENTORY=$INV make stop || echo "!! stop returned $?, continuing"
ANSIBLE_INVENTORY=$INV "$ANS" all -m shell -a \
  'pkill -f "yb-master|yb-tserver|arma|committer|loadgen" 2>/dev/null; sleep 2; true' -b >/dev/null 2>&1
say "confirming the database processes are gone"
ANSIBLE_INVENTORY=$INV "$ANS" all -m shell -a 'pgrep -c "yb-master|yb-tserver" || true' 2>&1 |
  grep -oE "^[0-9]+$" | sort -u | tr '\n' ' ' | xargs -I{} echo "    remaining per host: {}"

say "remove the CA and the monitoring containers so both are rebuilt from current config"
podman rm -f fca-org1 fca-org1-db prometheus grafana loki alloy >/dev/null 2>&1 || true
rm -rf /data1/fabric-x/fca-org1
podman unshare rm -rf /data1/fabric-x/fca-org1-db 2>/dev/null || rm -rf /data1/fabric-x/fca-org1-db
rm -rf out/control-node/fetched out/control-node/config

say "wipe the deploy volume"
ANSIBLE_INVENTORY=$INV make hard-wipe TARGET_HOSTS=all || echo "!! hard-wipe returned $?, continuing"
say "wipe the database, whose data dirs are SIBLINGS of /data1/fabric-x and survive hard-wipe"
ANSIBLE_INVENTORY=$INV "$ANS" all -m shell -a \
  'rm -rf /data1/yb-master /data1/yb-tserver /data2/yb-tserver /data2/fabric-x' -b >/dev/null 2>&1

say "gate: no database state left anywhere"
LEFT=$(ANSIBLE_INVENTORY=$INV "$ANS" all -m shell -a \
  'ls -d /data1/yb-master /data1/yb-tserver /data2/yb-tserver 2>/dev/null | wc -l' 2>&1 |
  grep -cE '^[1-9][0-9]*$')
if [ "$LEFT" != "0" ]; then echo "!! database state survived the wipe on $LEFT host(s); not continuing"; exit 1; fi
say "no database state survived the wipe"


say "restore staged binaries"
install -m 0750 -D /data1/bin-stage/committer /data1/bin-stage/loadgen \
  -t "$FX_PROJECT/out/control-node/bin/Linux/x86_64/" || echo "!! staging restore returned $?"

say "setup"
ANSIBLE_INVENTORY=$INV make setup || { echo "!! setup failed"; exit 1; }

say "gate: every certificate in every org tree"
bad=0
for D in out/control-node/fetched/crypto/peerOrganizations/* out/control-node/fetched/crypto/ordererOrganizations/*; do
  [ -d "$D" ] || continue
  CA=$(ls "$D"/msp/cacerts/*.pem 2>/dev/null | head -1); [ -n "$CA" ] || continue
  while read -r c; do
    [ -n "$c" ] || continue
    openssl verify -CAfile "$CA" "$c" >/dev/null 2>&1 || { echo "!! bad: ${c#out/control-node/fetched/crypto/}"; bad=$((bad+1)); }
  done < <(find "$D" -name '*.pem' | grep -vE 'cacerts|tlscacerts|keystore|tls/')
done
[ "$bad" -gt 0 ] && { echo "!! $bad certificate(s) do not verify; not starting"; exit 1; }
say "all certificates verify"

say "start"
ANSIBLE_INVENTORY=$INV make start || { echo "!! start failed"; exit 1; }

# The namespace does not exist yet, and on this arm the load generator cannot create it:
# loadgen_generate_namespace is false because a namespace-creation TX writes to _meta, whose
# policy is an MSP rule, and the loadgen role renders artifacts-path only for the mock orderer.
# So `make init` has fxconfig submit the envelopes through a router with the enrolled CA
# identity. Without this the pipeline runs perfectly and every TX is ABORTED_SIGNATURE_INVALID.
# Its summary line reports every non-loadgen host as skipped and still exits 0 -- read the
# namespace list, not the exit code.
# Only the end-to-end arm needs this. On the committer-only arm the load generator creates the
# namespace itself -- loadgen_generate_namespace is true there, because the mock-orderer path renders
# the artifacts-path that endorsing against _meta needs -- and the collection gates its init target on
# there being an ordering service anyway.
if grep -q "orderer_component_type" "$INV"; then
  say "init: create the namespace via fxconfig (the loadgen cannot on this arm)"
  ANSIBLE_INVENTORY=$INV make init || { echo "!! init failed"; exit 1; }
else
  say "init: skipped, no ordering service in this inventory -- the loadgen creates the namespace"
fi

say "gate: non-zero committed rate AND a present latency series"
for i in $(seq 1 40); do
  sleep 15
  c=$(curl -sk --max-time 8 -G https://localhost:9090/api/v1/query \
      --data-urlencode 'query=sum(rate(loadgen_transaction_committed_total[1m]))' |
      python3 -c 'import json,sys
try:
    r=json.load(sys.stdin)["data"]["result"]; print(float(r[0]["value"][1]) if r else 0)
except Exception: print(0)')
  p=$(curl -sk --max-time 8 -G https://localhost:9090/api/v1/query \
      --data-urlencode 'query=histogram_quantile(0.5, sum by (le) (rate(loadgen_valid_transaction_latency_seconds_bucket[1m])))' |
      python3 -c 'import json,sys
try:
    r=json.load(sys.stdin)["data"]["result"]; print(float(r[0]["value"][1]) if r else float("nan"))
except Exception: print(float("nan"))')
  echo "    probe $i: committed/s=$c p50=$p"
  case "$p" in *nan*) continue;; esac
  awk -v c="$c" 'BEGIN{exit !(c>0)}' && { say "GATE PASSED: committing $c tx/s, p50 $p s"; exit 0; }
done
echo "!! never committed; check the loadgen log for ABORTED_SIGNATURE_INVALID"
exit 1
