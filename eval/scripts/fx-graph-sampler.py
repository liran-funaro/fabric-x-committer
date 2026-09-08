#!/usr/bin/env python3
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
"""
Sample the coordinator's dependency graph while a figures run is in flight.

The paper attributes its 41% throughput drop from one input/output to four to the coordinator's
dependency graph -- "a synchronization primitive protecting the graph serializes access, creating a
contention point that limits throughput". This samples the two metrics that test that claim directly,
as lock-wait *utilisation*: the seconds each goroutine spends waiting for the graph's mutex per
second of wall clock. At 1.0 the mutex is the serialiser; at 0.05 it is not.

It runs beside fx-figures.py rather than inside it, so it needs no restart of a run already going,
and joins to the probes by timestamp.
"""
import json
import os
import subprocess
import time

PROM = os.environ.get("FX_PROM", "https://localhost:9090")
OUT = os.environ.get("FX_GRAPH_OUT", "/data1/logs/graph.jsonl")
INTERVAL = int(os.environ.get("FX_GRAPH_INTERVAL", "30"))
W = "60s"

GDG = "coordinator_global_dependency_graph"
QUERIES = {
    # Seconds of lock wait per second of wall clock, per goroutine. The constructor holds the graph
    # to add a batch; the validated-tx processor holds it to remove dependents.
    "ctor_lock_util":  f"sum(rate({GDG}_constructor_wait_for_lock_seconds_sum[{W}]))",
    "proc_lock_util":  f"sum(rate({GDG}_validated_tx_batch_processor_wait_for_lock_seconds_sum[{W}]))",
    "ctor_lock_ms":    (f"1000 * sum(rate({GDG}_constructor_wait_for_lock_seconds_sum[{W}]))"
                        f" / sum(rate({GDG}_constructor_wait_for_lock_seconds_count[{W}]))"),
    "proc_lock_ms":    (f"1000 * sum(rate({GDG}_validated_tx_batch_processor_wait_for_lock_seconds_sum[{W}]))"
                        f" / sum(rate({GDG}_validated_tx_batch_processor_wait_for_lock_seconds_count[{W}]))"),
    # What the graph does once it has the lock, so lock wait can be read against real work.
    "construct_util":  f"sum(rate({GDG}_construction_seconds_sum[{W}]))",
    "add_batch_util":  f"sum(rate({GDG}_add_tx_batch_to_graph_seconds_sum[{W}]))",
    "detector_util":   f"sum(rate({GDG}_update_dependency_detector_seconds_sum[{W}]))",
    "graph_size":      f"sum({GDG}_size)",
    "in_queue":        f"sum({GDG}_input_tx_batch_queue_size)",
    "dependent_queue": "sum(coordinator_dependency_graph_dependent_transactions_queue_size)",
    "tx_processed":    f"sum(rate({GDG}_tx_processed_total[{W}]))",
    "committed":       f"sum(rate(loadgen_transaction_committed_total[{W}]))",
    # The generator keeps two latency histograms and the driver only reads one: valid_* covers
    # transactions that COMMITTED, invalid_* covers those that were rejected. On the invalid
    # signature and double spend sweeps a tenth to a third of the workload lands in the second one,
    # so the reported tail is the committed population's and this is the rest of it.
    "lat_p99_invalid": ("histogram_quantile(0.99, sum by (le) "
                        f"(rate(loadgen_invalid_transaction_latency_seconds_bucket[{W}])))"),
    "lat_p50_invalid": ("histogram_quantile(0.50, sum by (le) "
                        f"(rate(loadgen_invalid_transaction_latency_seconds_bucket[{W}])))"),
    # Transactions per database batch, which with the transaction's key count gives the keys per
    # multi-key lookup. YugabyteDB batches such a lookup into per-tablet requests only while
    # tablets x keys stays under about 32,768; at this cluster's 120-way pre-split that is ~273 keys
    # per lookup, above which it issues one storage read per key. So this number times the read-write
    # count decides which side of that cliff a workload sits on, and it is the measurement that
    # separates "large transactions are expensive" from "this batch width crossed a threshold".
    # Transactions per READ-VALIDATION call is the one that matters: validateNamespaceReads passes
    # every read key of a batch for a namespace in one array, with no chunking, so this times the
    # read-write count is the key count the cliff applies to. NOTE the numerator is the COMMITTED
    # rate, so this is only transactions-per-validation on a workload where nothing is rejected. With
    # 30% invalid signatures it read 247.9 against 376.7 at 0%, which is the 30% that never reached
    # validation rather than a narrower batch. Read it only on the no-rejection points.
    "tx_per_validation": (f"sum(rate(vcservice_committed_transaction_total[{W}]))"
                          f" / sum(rate(vcservice_database_tx_batch_validation_latency_seconds_count[{W}]))"),
    "tx_per_db_batch": (f"sum(rate(vcservice_committed_transaction_total[{W}]))"
                        f" / sum(rate(vcservice_database_tx_batch_commit_latency_seconds_count[{W}]))"),
    "db_batches_s":    f"sum(rate(vcservice_database_tx_batch_commit_latency_seconds_count[{W}]))",
    # Commit latency and the table's size, sampled together every 30 s. Within one hold the rate is
    # fixed while the table grows, so this pair is the controlled test of whether fill costs latency
    # -- and sampling it continuously beats range-querying the window afterwards, because teardown
    # takes Prometheus and the window's series with it seconds after the hold ends.
    "db_commit_ms":    (f"1000 * sum(rate(vcservice_database_tx_batch_commit_latency_seconds_sum[{W}]))"
                        f" / sum(rate(vcservice_database_tx_batch_commit_latency_seconds_count[{W}]))"),
    "committed_total": "sum(loadgen_transaction_committed_total)",
    "validation_ms":   (f"1000 * sum(rate(vcservice_database_tx_batch_validation_latency_seconds_sum[{W}]))"
                        f" / sum(rate(vcservice_database_tx_batch_validation_latency_seconds_count[{W}]))"),
    "validation_util": f"sum(rate(vcservice_database_tx_batch_validation_latency_seconds_sum[{W}]))",
    "commit_util":     f"sum(rate(vcservice_database_tx_batch_commit_latency_seconds_sum[{W}]))",
}

# Where the ceiling sits, which the driver only records as one busiest host. Recorded here as the
# three busiest machines with their names, because "commit6 at 79%" and "commit6, commit7, commit8
# all at 79%" mean different things: the first is one hot machine, the second is a saturated tier.
TOP_CPU = ("topk(3, 1 - avg by (instance) "
           f"(rate(node_cpu_seconds_total{{mode=\"idle\"}}[{W}])))")


def query(expr):
    cmd = ["curl", "-sk", "--max-time", "15", "-G",
           "--data-urlencode", f"query={expr}", f"{PROM}/api/v1/query"]
    try:
        payload = json.loads(subprocess.run(
            cmd, capture_output=True, text=True, timeout=20).stdout)
    except (subprocess.TimeoutExpired, json.JSONDecodeError):
        return None
    result = payload.get("data", {}).get("result") or []
    if not result:
        return None
    value = float(result[0]["value"][1])
    return None if value != value else value


def top_cpu():
    cmd = ["curl", "-sk", "--max-time", "15", "-G",
           "--data-urlencode", f"query={TOP_CPU}", f"{PROM}/api/v1/query"]
    try:
        payload = json.loads(subprocess.run(
            cmd, capture_output=True, text=True, timeout=20).stdout)
    except (subprocess.TimeoutExpired, json.JSONDecodeError):
        return []
    return [[r.get("metric", {}).get("instance"), round(float(r["value"][1]), 3)]
            for r in payload.get("data", {}).get("result") or []]


def main():
    while True:
        row = {"at": time.time()}
        row.update({name: query(expr) for name, expr in QUERIES.items()})
        row["top_cpu"] = top_cpu()
        with open(OUT, "a") as f:
            f.write(json.dumps(row) + "\n")
        time.sleep(INTERVAL)


if __name__ == "__main__":
    main()
