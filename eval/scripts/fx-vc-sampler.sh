#!/usr/bin/env bash
# Record the VC counters that price the failure path, every 30 s, for every validator-committer.
#
# Read-only: one curl of /metrics per VC per interval, no state touched. It exists because the tablet
# boundary test needs batch width sampled IN-WINDOW at each tablet count -- width follows the downstream
# service rate, so a tablet change that moves throughput moves width and therefore moves the predicted
# threshold crossing. Assuming a fixed 349 keys can make a holding model look falsified.
#
# Columns are cumulative counters. Deltas between any two lines give width, calls per commit and insert
# latency for that window, which is what makes a run interpretable after the fact rather than only while
# someone is watching it.
set -u
# Absolute paths, deliberately: a bring-up deletes and recreates the Prometheus config directory, and a
# process that had cd'd into it is left on an unlinked inode -- every curl then fails silently and the
# sampler logs nothing while appearing to run. That cost 45 minutes of live measurement once, and again
# across the tab96 bring-up. Resolving the certs afresh each time survives a recreate.
CFG=/data1/fabric-x/prometheus/config
C="--cert $CFG/tls/server.crt --key $CFG/tls/server.key"
echo "ts host committed commits insert_count insert_sum conflicts"
while true; do
  for ip in 13 14 15 16 17 18; do
    m=$(curl -sk $C -m 5 "https://10.241.64.$ip:5200/metrics" 2>/dev/null)
    [ -z "$m" ] && continue
    v() { echo "$m" | awk -v k="$1" '$1==k {print $2; f=1} END{if(!f) print "-"}'; }
    echo "$(date +%H:%M:%S) vc$ip \
$(v vcservice_committed_transaction_total) \
$(v vcservice_database_tx_batch_commit_latency_seconds_count) \
$(v vcservice_database_tx_batch_commit_insert_new_key_with_value_latency_seconds_count) \
$(v vcservice_database_tx_batch_commit_insert_new_key_with_value_latency_seconds_sum) \
$(v vcservice_mvcc_conflict_total)"
  done
  sleep 30
done
