#!/usr/bin/env bash
# Sample every tier's queue gauges on one clock, every 5 s, while a measurement runs.
#
# Absolute paths, re-resolved each iteration. An earlier version cd'd into the Prometheus config
# directory once at startup; a per-point teardown deletes and recreates that directory, so the process
# was left holding a dead inode and every sample came back as a dash -- silently, for a whole ladder.
set -u
CFG=/data1/fabric-x/prometheus/config
echo "ts sc_waiting sc_height co_verif_in co_verif_out co_vc_in co_vc_out co_dep co_graph vc_committed vc_conflicts"
while true; do
  if [ -r "$CFG/tls/server.crt" ]; then
    C="--cert $CFG/tls/server.crt --key $CFG/tls/server.key"
    sc=$(curl -sk $C -m 5 https://10.241.64.8:5230/metrics 2>/dev/null)
    co=$(curl -sk $C -m 5 https://10.241.64.7:5220/metrics 2>/dev/null)
    vc=$(curl -sk $C -m 5 https://10.241.64.13:5200/metrics 2>/dev/null)
  else
    sc=""; co=""; vc=""   # between a teardown and its setup: nothing to scrape, and that is data too
  fi
  v() { echo "$1" | awk -v k="$2" '$1==k {print $2; f=1} END{if(!f) print "-"}'; }
  echo "$(date +%H:%M:%S) \
$(v "$sc" sidecar_relay_waiting_transactions_queue_size) \
$(v "$sc" sidecar_ledger_block_height) \
$(v "$co" coordinator_verifier_input_batch_queue_size) \
$(v "$co" coordinator_verifier_output_batch_queue_size) \
$(v "$co" coordinator_vcservice_input_batch_queue_size) \
$(v "$co" coordinator_vcservice_output_batch_queue_size) \
$(v "$co" coordinator_dependency_graph_dependent_transactions) \
$(v "$co" coordinator_global_dependency_graph_size) \
$(v "$vc" vcservice_committed_transaction_total) \
$(v "$vc" vcservice_mvcc_conflict_total)"
  sleep 5
done
