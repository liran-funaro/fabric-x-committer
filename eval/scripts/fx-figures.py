#!/usr/bin/env python3
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
"""
Recreate the Fabric-X paper's committer figures on this cluster, unattended.

Runs ON the monitor. One experiment is one workload shape, and every experiment gets its own
deployment: `make teardown` drops the runtime state, `make configs` re-renders the load generator
with that shape, and `make start` brings it back. That matters more here than anywhere else in
this project -- a step past the knee does not drain within a run (see the README), and the
transaction shape also decides how much the database holds, so carrying either across experiments
would put the previous shape's queue and rows into this shape's numbers.

Per experiment the question is the paper's: the highest rate whose 99th percentile latency stays
under a second, with the offered rate arriving and no queue building behind it. A probe that
meets all four conditions steps up, one that misses steps down, and the best rate is then held
for a final window long enough to report -- the paper averages each point over at least five
minutes, so the confirmation hold is 300 s.

Every probe is also a data point in its own right: it pairs a latency with the throughput that
produced it, which is the second figure. The `curve` mode exists to sweep that deliberately, at
a fixed rate ladder rather than a search.

Results append to /data1/logs/figures.jsonl, one JSON object per probe or hold, so a crash or a
kill loses at most the running probe. The plotting script reads only that file.
"""
import json
import os
import subprocess
import sys
import time

PROM = os.environ.get("FX_PROM", "https://localhost:9090")
PROJECT = os.environ.get(
    "FX_PROJECT", "/data1/collections/ansible_collections/hyperledger/fabricx")
INVENTORY = os.environ.get("FX_INVENTORY", "/data1/cluster/inventory/cluster.yaml")
# The two arms must not share a results file: the e2e ladders are named curve* like the committer's
# own curve, so a shared file would silently merge two different experiments into one plot. Default
# by matrix rather than relying on every caller to remember FX_OUT.
OUT = os.environ.get(
    "FX_OUT",
    "/data1/logs/figures-orderer.jsonl" if os.environ.get("FX_MATRIX") == "e2e"
    else "/data1/logs/figures.jsonl")
VARS_FILE = "/data1/logs/exp-vars.yaml"

# The top finite bucket of loadgen_valid_transaction_latency_seconds. Above it `histogram_quantile`
# reports this number instead of a quantile. Hardcoded rather than queried because the bucket set is a
# constant in the load generator's code, and asserted against below so a change cannot pass silently.
P99_CEILING = float(os.environ.get("FX_P99_CEILING", "60.0"))

WINDOW = "60s"
# The paper keeps every reported point under a second of 99th percentile latency, and treats
# that as the condition for the rate being usable rather than merely achievable.
SLO_P99 = float(os.environ.get("FX_SLO_P99", "1.0"))
TOLERANCE = float(os.environ.get("FX_TOLERANCE", "0.02"))
# Below this much free space on any data disk, stop rather than fill a disk as the last run did.
DISK_FLOOR_GB = int(os.environ.get("FX_DISK_FLOOR_GB", "120"))

QUERIES = {
    "offered":   f"sum(rate(loadgen_transaction_sent_total[{WINDOW}]))",
    "committed": f"sum(rate(loadgen_transaction_committed_total[{WINDOW}]))",
    "aborted":   f"sum(rate(loadgen_transaction_aborted_total[{WINDOW}]))",
    "sent_total":      "sum(loadgen_transaction_sent_total)",
    "committed_total": "sum(loadgen_transaction_committed_total)",
    "aborted_total":   "sum(loadgen_transaction_aborted_total)",
    "sc_waiting": "sum(sidecar_relay_waiting_transactions_queue_size)",
    "vc_commit":  f"sum(rate(vcservice_committed_transaction_total[{WINDOW}]))",
    "mvcc":       "sum(vcservice_mvcc_conflict_total)",
    "blk_rate":   f"sum(rate(sidecar_ledger_append_block_seconds_count[{WINDOW}]))",
    "append_util": f"sum(rate(sidecar_ledger_append_block_seconds_sum[{WINDOW}]))",
    "lat_mean":   (f"sum(rate(loadgen_valid_transaction_latency_seconds_sum[{WINDOW}]))"
                   f" / sum(rate(loadgen_valid_transaction_latency_seconds_count[{WINDOW}]))"),
    "lat_p50":    ("histogram_quantile(0.50, sum by (le) "
                   f"(rate(loadgen_valid_transaction_latency_seconds_bucket[{WINDOW}])))"),
    "lat_p99":    ("histogram_quantile(0.99, sum by (le) "
                   f"(rate(loadgen_valid_transaction_latency_seconds_bucket[{WINDOW}])))"),
    "db_commit":  (f"sum(rate(vcservice_database_tx_batch_commit_latency_seconds_sum[{WINDOW}]))"
                   f" / sum(rate(vcservice_database_tx_batch_commit_latency_seconds_count[{WINDOW}]))"),
    # `db_commit` does NOT cover the insert step, and assuming it did cost two days. During the conflict
    # collapse it reads 22.9 ms while the insert on the same VC in the same window reads 1.72 SECONDS --
    # 75x. Every log that said "the database is healthy at 17-28 ms" was reading a metric that excludes
    # the operation doing the work, which sent three sessions after the coordinator, the dependency
    # graph, the tablet split and the reference gap instead. Measure the insert directly.
    "db_insert":  (f"sum(rate(vcservice_database_tx_batch_commit_insert_new_key_with_value_latency_seconds_sum[{WINDOW}]))"
                   f" / sum(rate(vcservice_database_tx_batch_commit_insert_new_key_with_value_latency_seconds_count[{WINDOW}]))"),
    # Inserts per commit: 1.0 when nothing conflicts, and the retry loop in committer.go re-running a
    # batch after `insert_ns` raises unique_violation when something does. Measured at 1.91 during the
    # collapse. Recording it means a future run shows the retry rather than leaving it to be inferred.
    # Transactions per insert CALL, which is NOT the batch width: `insertStates` is called once per commit
    # ATTEMPT, and a conflicting batch attempts ~1.8 times (`db_insert_per_commit`), so this quotient is
    # width / attempts. Multiply the two to recover the width -- 84 x 1.79 = 150 transactions, ~300 keys,
    # not the 168 that reading this series directly gives. Both factors are recorded per row, so any
    # analysis can correct it, but three separate models were fitted to the uncorrected figure first and
    # the correction moves the fit: at 88 tablets it takes keys x tablets from 14,784 to 26,400.
    # The threshold that matters
    # is tablets x keys-per-lookup, and this width is NOT the graph's chunk size: measured median 152
    # against a chunk of 500, range 1-418, and it collapses from ~300 to ~125-150 under the very
    # conditions being measured. A tablet sweep without it cannot be interpreted -- every point could sit
    # on the same side of the threshold and look like a null result.
    "tx_per_insert": (
        f"sum(rate(vcservice_committed_transaction_total[{WINDOW}]))"
        f" / sum(rate(vcservice_database_tx_batch_commit_insert_new_key_with_value_latency_seconds_count[{WINDOW}]))"),
    "db_insert_per_commit": (
        f"sum(rate(vcservice_database_tx_batch_commit_insert_new_key_with_value_latency_seconds_count[{WINDOW}]))"
        f" / sum(rate(vcservice_database_tx_batch_commit_latency_seconds_count[{WINDOW}]))"),
    "cpu_max":    ("max(1 - avg by (instance) "
                   f"(rate(node_cpu_seconds_total{{mode=\"idle\"}}[{WINDOW}])))"),
    "cpu_busiest": ("topk(1, 1 - avg by (instance) "
                    f"(rate(node_cpu_seconds_total{{mode=\"idle\"}}[{WINDOW}])))"),
    "verifier_cpu": ("max(1 - avg by (instance) (rate(node_cpu_seconds_total"
                     f"{{mode=\"idle\",instance=~\"verifier.*\"}}[{WINDOW}])))"),
    "coord_cpu":  ("max(1 - avg by (instance) (rate(node_cpu_seconds_total"
                   f"{{mode=\"idle\",instance=~\"coordinator\"}}[{WINDOW}])))"),
    "loadgen_cpu": ("max(1 - avg by (instance) (rate(node_cpu_seconds_total"
                    f"{{mode=\"idle\",instance=~\"loadgen\"}}[{WINDOW}])))"),
    "down":       "count(up == 0) or vector(0)",
    "disk_free_gb": 'min(node_filesystem_avail_bytes{mountpoint=~"/data.*"}) / 1e9',
}

# The transaction shape. A settlement transaction spends `inputs` keys and creates `outputs`
# keys, so an input is a read-write operation (the key is read, then overwritten as spent) and
# an output is a blind write (a new key, written without reading -- which is what the validator
# resolves with its own version lookup). n/n therefore touches 2n keys, as the paper's does.
def shape(inputs, outputs, invalid=0.0, backref=0.0, block=None):
    v = {
        "loadgen_generate_read_write_tx": True,
        "loadgen_read_write_tx_keys": inputs,
        "loadgen_generate_write_only_tx": outputs > 0,
        "loadgen_write_only_tx_keys": outputs,
        "loadgen_conflicts_settings": {"invalid_signatures": invalid},
        "loadgen_key_backref_rate": backref,
    }
    if block:
        # The tuned configuration cuts 10,000-transaction blocks, which buys throughput and pays
        # for it in latency: nothing in a block moves until the block is cut. The paper reports
        # tens of milliseconds at 400,000 tps, which a block that large cannot produce, so the
        # block size is the knob that decides which end of that trade a point sits at.
        v["loadgen_block_max_size"] = block
    if backref:
        # `key_backref_rate` is the double-spend rate: at 0.05, five per cent of transactions put an
        # existing key in a read-write slot with a nil expected version, which read validation rejects
        # as ABORTED_MVCC_CONFLICT -- the same path a real double spend trips.
        #
        # The gap is how far back, in TRANSACTIONS, a reference reaches, and the lookback window is
        # drawn downward from the frontier as it stood there. What has to match the paper is not the
        # gap but what the gap buys: the share of references that name a key whose creating transaction
        # has not committed yet. Those are dependencies the coordinator must order, not conflicts it
        # can reject, and they are why gap 0 measured a convoy instead of double spends.
        #
        # In flight is rate x latency x fresh keys per transaction. At 518,000 tps the committer's mean
        # latency is 345 ms and its p99 457 ms, so 304,000 to 402,000 keys have been handed out and not
        # committed. A gap has to clear the larger of those, and 300,000 transactions is 510,000 keys.
        # Measured over the real generator at this shape: gap 1,000 (the paper's own value, whose
        # pipeline is 85 ms and whose window is therefore ~60,500 keys) puts 40% of references inside
        # this deployment's window, gap 225,000 puts 2.0%, and gap 300,000 puts none.
        #
        # Erring long is the safe direction: a reference to an older key is still a double spend, while
        # one to an uncommitted key is a dependency the coordinator must order and not a conflict it can
        # reject. That is what made the first 9c measurements a convoy rather than a conflict result.
        v["loadgen_tx_reference_gap"] = 300_000
        v["loadgen_key_lookback_window"] = 1_000_000
    return v


BASE_SEED = int(os.environ.get("FX_SEED", "480000"))
# What a fresh point costs to deploy. "configs" re-renders configuration only, which is all the
# committer-only arm needs; "setup" also rebuilds binaries, crypto and genesis blocks, which the
# real-orderer arm does need. See deploy().
DEPLOY_PLAN = os.environ.get("FX_DEPLOY_PLAN", "configs")
# The groups a per-point redeploy tears down: the committers, the load generator, and on the
# real-orderer arm the ordering service too. Never fabric_cas, never monitoring -- see deploy().
# Narrowing DOES work, with colons rather than commas. Each teardown play selects with
# `{{ target_hosts }}:&<its own group>`, and Ansible applies that intersection to the accumulated set
# left to right: `a:b:&c` is (a or b) and c, while `a,b:&c` is a or (b and c). A comma therefore leaves
# the first term unintersected, which is what put every committer host through the ORDERER role's
# teardown and failed all 19 with "missing required arguments: orderer_component_type". Naming the
# `fabric_x` parent instead of `fabric_x_committers` compounded it, since the parent spans both arms.
#
# Verified by hand against this inventory: `fabric_x_committers:load_generators` exits 0 with no host
# reporting a failure, and leaves the Fabric CA and the monitoring stack standing. `all` also works but
# takes both down every point, which costs Prometheus history, flaps Grafana, and re-enrols the CA -- the
# "Authentication failure" that killed a six-point size sweep.
#
# THE SEPARATOR IS LOAD-BEARING. Use colons, never commas. Each play in the teardown composes its own
# group into the pattern as `{{ target_hosts }}:&<its group>`, and a comma binds looser than that
# intersection: `a,b:&c` is a OR (b AND c), while `a:b:&c` is (a OR b) AND c. So `fabric_x,load_generators`
# gave the monitoring play `load_generators:&monitoring`, which matched nothing, and gave the loadgen play
# all 26 committer hosts instead of one -- every host then failed with "missing required arguments:
# orderer_component_type" and the driver aborted two ladders having measured nothing.
#
# It is not the group name: `ansible-inventory --list` shows `fabric_x` and `fabric_x_committers`
# resolving to the same 26 hosts, which is what proves the separator was the whole of it.
TEARDOWN_HOSTS = os.environ.get(
    "FX_TEARDOWN_HOSTS",
    "fabric_x_committers:load_generators" if os.environ.get("FX_MATRIX") != "e2e"
    else "fabric_x_committers:load_generators:fabric_x_orderers")
# Measure on the deployment that is already running, rather than replacing it. The shape is still read
# back and still has to match, so this cannot silently measure the wrong workload -- it only skips the
# teardown. For an arm that is expensive or fragile to bring up, that is the difference between measuring
# and starting over: the real-orderer arm took six attempts to start, and its ladder needs no shape change.
SKIP_DEPLOY = os.environ.get("FX_SKIP_DEPLOY") == "1"
# Points to measure again even though the output already holds a confirmed hold for them. The first
# two points of the size sweep were measured before the driver held its confirmation on a fresh
# deployment, so they ran against a table holding several times as many rows as the later points --
# and commit latency rises with the table, at about 0.18 ms per million transactions committed.
REDO = set(filter(None, os.environ.get("FX_REDO", "").split(",")))
# Sixteen 8% steps is a 3.43x climb from the seed. Four (1.36x) and then ten (2.16x) were both too
# few, and for a reason worth stating rather than patching around: the size sweep's seeds are the
# paper's own ratios against its 300 B point, and the paper's sweep is bandwidth-bound near
# 120 MB/s past 300 B. This deployment is not -- 1024 B transactions sustain 292 MB/s here -- so
# every seed derived that way is low by more than a factor of two, and the climb rather than the
# system was setting the reported number. The log line to look for is a search whose LAST probe met
# its rate: that is a lower bound, not a knee. Unmet steps cost one probe each and only until the
# first miss, so a seed that was already close pays almost nothing for the headroom.
UP_STEPS = int(os.environ.get("FX_UP_STEPS", "16"))

EXPERIMENTS = [
    # Figure 9a: throughput and latency against transaction size. n read-write operations per
    # transaction: n keys read and written, which is one input spent and one output written per
    # operation, at the same key. This is the shape the paper's own generator has a knob for, and
    # the shape every number this cluster has published uses, so it is the comparable series.
    dict(id="9a-rw1", figure="9a", x=1, label="1 read-write", seed=BASE_SEED, vars=shape(1, 0)),
    dict(id="9a-rw2", figure="9a", x=2, label="2 read-writes", seed=BASE_SEED, vars=shape(2, 0)),
    dict(id="9a-rw3", figure="9a", x=3, label="3 read-writes", seed=420000, vars=shape(3, 0)),
    dict(id="9a-rw4", figure="9a", x=4, label="4 read-writes", seed=380000, vars=shape(4, 0)),
    # Figure 9b: invalid signatures, at two read-writes.
    dict(id="9b-inv0", figure="9b", x=0, label="0% invalid", seed=BASE_SEED,
         vars=shape(2, 0, invalid=0.0)),
    dict(id="9b-inv10", figure="9b", x=10, label="10% invalid", seed=BASE_SEED,
         vars=shape(2, 0, invalid=0.1)),
    dict(id="9b-inv20", figure="9b", x=20, label="20% invalid", seed=BASE_SEED,
         vars=shape(2, 0, invalid=0.2)),
    dict(id="9b-inv30", figure="9b", x=30, label="30% invalid", seed=BASE_SEED,
         vars=shape(2, 0, invalid=0.3)),
    # Figure 9c: double spends, at two read-writes. A back-reference puts an already-committed key
    # in a read-write slot with a nil expected version, and validate_reads_ns flags exactly that
    # ("the key exists in the committed state but the expected version is null"), so the
    # transaction is rejected as ABORTED_MVCC_CONFLICT by the same read validation a real double
    # spend trips, and its writes are dropped before the commit stage.
    #
    # Every share runs at the deployment's own 120-way tablet pre-split, which is what the paper used
    # too, so the panel is one workload variable against one published series. `key_backref_rate` IS
    # the share: 0.05 is a 5% double spend.
    dict(id="9c-ds0", figure="9c", x=0, label="0% double spend", seed=BASE_SEED,
         vars=shape(2, 0, backref=0.0)),
    dict(id="9c-ds5", figure="9c", x=5, label="5% double spend", seed=BASE_SEED,
         vars=shape(2, 0, backref=0.05)),
    dict(id="9c-ds10", figure="9c", x=10, label="10% double spend", seed=BASE_SEED,
         vars=shape(2, 0, backref=0.10)),
    dict(id="9c-ds20", figure="9c", x=20, label="20% double spend", seed=BASE_SEED,
         vars=shape(2, 0, backref=0.20)),
    dict(id="9c-ds30", figure="9c", x=30, label="30% double spend", seed=BASE_SEED,
         vars=shape(2, 0, backref=0.30)),

    # Why the conflict workload collapses, as three falsifiable variants of one 5% point. Every
    # conflict share at every gap lands near 20,000 tps against 518,000 conflict-free, rate-independent,
    # with the busiest DATABASE node at 73% CPU -- so the suspect is not the rate and not the
    # coordinator.
    #
    # The prime suspect is YugabyteDB's multi-key read batching. `key = ANY(array)` is batched into
    # per-tablet requests only while tablets x keys-per-lookup stays under ~32,768; above it, one
    # storage read request per key. `database.validateNamespaceReads` passes EVERY read key of a
    # validation batch in one array with no chunking, so keys per lookup is (transactions in the batch)
    # x (keys per transaction) -- thousands, against 120 tablets. It is invisible on a conflict-free
    # workload because inserting fresh keys performs no multi-key lookup; a back-reference is the first
    # thing that does. The same cliff took a blind-write workload from 314,336 to 13,160 tps.
    #
    # Two ways to stay under the cliff, and they predict the same outcome for different reasons: fewer
    # tablets, or a narrower batch. If either recovers the rate, the cliff is the mechanism. If neither
    # does, the third variant asks whether it is the dependency-graph manager instead -- this deployment
    # runs the simple one and the paper ran the global one, and a read-write reference is a WRITER on
    # its key there, which never joins a running group.
    #
    # An earlier chunk-width test was read as refuting the batching explanation. It ran the
    # conflict-FREE shape, where the cliff cannot appear, so it refuted nothing.
    dict(id="9c-ds5-split8", figure="conflict-why", x=8, label="5% double spend, 8 tablets",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_database_table_pre_split_tablets=8)),
    dict(id="9c-ds5-chunk64", figure="conflict-why", x=64, label="5% double spend, 64-tx chunks",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_coordinator_dep_graph_chunk_size=64)),
    dict(id="9c-ds5-gdg", figure="conflict-why", x=0, label="5% double spend, global graph",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_coordinator_dep_graph_use_simple_manager=False)),

    # The reference gap has to clear the DEPENDENCY GRAPH's window, not the latency one. The graph holds
    # `wait-tx-limit` transactions -- 500,000 here, against the code default of 100,000 -- and a
    # reference reaching back less than that names a key whose creating transaction is still inside the
    # graph. That is a dependency to order, not a conflict to reject, and the sampler shows exactly what
    # ordering them looks like: the graph pinned at 500,000, every graph stage under 3% utilisation, the
    # database at 17-28 ms with 2 of 192 commit workers busy, and released batches down from ~300
    # transactions to 72. Nothing saturated, everything waiting.
    #
    # Two ways to make a reference land on a committed key, and they should agree:
    #   - lower the graph's window to the default the published run used;
    #   - raise the gap past the window this deployment configures.
    dict(id="9c-ds5-graph100k", figure="conflict-why", x=100, label="5% double spend, 100k graph limit",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_coordinator_dep_graph_wait_tx_limit=100_000)),
    dict(id="9c-ds5-gap1m", figure="conflict-why", x=1000, label="5% double spend, 1M reference gap",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   loadgen_tx_reference_gap=1_000_000)),

    # Raise the graph's limit rather than lower it, which is the prediction that separates a soft bound
    # from a cliff.
    #
    # The cross-tier sampler shows the graph filling to its limit with work that is NOT blocked on keys:
    # 500,000 admitted-and-unvalidated against 15-200 dependent. Over the limit `taskProcessing` sets its
    # admission channel to nil, so its single select admits exactly one batch per validated batch -- the
    # pipeline goes lock-step and throughput becomes batch over round trip, about 100 transactions in
    # 25 ms, which is the 20,000 tps every conflict share reports. A conflict-free run never crosses the
    # limit (179,000 in flight at 518,000 tps and 345 ms) and so never enters that regime; aborts add
    # enough latency to cross it, and once crossed there is no way back.
    #
    # If that is right, a limit the run cannot reach restores normal throughput, and 100,000 -- the role
    # default the published deployment used -- should be WORSE than 500,000 rather than better. The first
    # half is this experiment; the second half already measured, at 14,364 tps against 20,000.
    dict(id="9c-ds5-graph5m", figure="conflict-why", x=5000, label="5% double spend, 5M graph limit",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_coordinator_dep_graph_wait_tx_limit=5_000_000)),

    # The same cliff avoided from the other side: cap what can be outstanding rather than raise what the
    # graph will hold. The sidecar releases up to `waiting-txs-limit` transactions before it waits for
    # status, and that limit is 500,000 -- exactly the graph's. So the inflow is free to fill the graph to
    # its limit and tip it into lock-step. Hold the sidecar to 200,000 and the graph cannot reach 500,000,
    # so it should stay pipelined without any more memory in the coordinator.
    #
    # If this and the 5,000,000 graph limit both restore throughput, the cliff is the mechanism and either
    # value is a fix. If only one does, the difference says which side the pressure comes from.
    # The only uncontaminated latency available for this workload. Every 120-tablet latency measured so
    # far is a backlog's age rather than a cost: the drain parked at 20,000 against a capacity of 20,235,
    # so rungs inherited millions of queued transactions and reported 148,836 ms, which at 23,000 tps is
    # simply 3.4M in flight. The 8-tablet figures are no better -- a 90 s probe at 239 ms and a 300 s hold
    # at 26 s. These rungs all sit BELOW the ~20,235 capacity, so with the drain fixed they measure what a
    # double spend costs a transaction rather than what a queue costs it.
    dict(id="9c-ds5-ladderlow", figure="conflict-ladder", x=120, mode="curve",
         label="5% double spend, 120 tablets",
         rates=[2500, 5000, 10000, 15000, 20000],
         vars=shape(2, 0, backref=0.05)),

    # The same measurement three times, because nothing else in this panel can be interpreted without it.
    # The five ladder rungs delivered 21,273 / 24,182 / 22,000 / 18,000 / 25,273 with no trend in the
    # offered rate and no trend in fill -- the fullest table gave the highest throughput. That is a spread
    # of a third of the mean against the 8.8% repeatability everything else here is held to, and it refutes
    # both explanations offered for it (fill, and capacity eroding under overload).
    #
    # Until the spread is a number, a tablet sweep cannot be read: tab88 against tab96 differing by less
    # than about a third would be indistinguishable from scatter. Three fresh deployments at one rate turn
    # the puzzle into an interval. Identical vars, so the plotting pools them and the spread is what it
    # pools.
    dict(id="9c-ds5-rep1", figure="conflict-repeat", x=1, mode="curve",
         label="5% double spend, repeat 1", rates=[100000], vars=shape(2, 0, backref=0.05)),
    dict(id="9c-ds5-rep2", figure="conflict-repeat", x=2, mode="curve",
         label="5% double spend, repeat 2", rates=[100000], vars=shape(2, 0, backref=0.05)),
    dict(id="9c-ds5-rep3", figure="conflict-repeat", x=3, mode="curve",
         label="5% double spend, repeat 3", rates=[100000], vars=shape(2, 0, backref=0.05)),

    # Cost per failing batch, or number of failing batches? No tablet count can tell those apart, because
    # both scale the same way with keys x tablets. The conflict share does: at ~179 transactions a batch,
    # 5% means essentially every batch takes the unique_violation path, 1% means 94% of them, and 0.1% only
    # a quarter. If the cost is per failing batch, throughput should recover roughly as the failing fraction
    # falls -- near-linearly by 0.1%. If it does not, the cost is attached to something else.
    #
    # Seeds chosen for where each is expected to land: 0.1% should recover several fold, 1% should sit near
    # the 5% figure.
    dict(id="9c-ds1", figure="9c", x=1, label="1% double spend", seed=30_000,
         vars=shape(2, 0, backref=0.01)),
    # Seeded low deliberately: this point's capacity is what is being tested and the two hypotheses predict
    # it six times apart. From 30,000 the search descends to 11,313 and can climb to 102,778, so one seed
    # brackets both a ~20,400 outcome and a ~100,000 one. At 150,000 the floor is 56,572 -- above the
    # pessimistic capacity, so the run could only ever have reported "no rate met" in exactly the case that
    # refutes recovery. Note an explicit seed here is NOT overridable by FX_SEED, which only feeds BASE_SEED.
    dict(id="9c-ds01", figure="9c", x=0.1, label="0.1% double spend", seed=30_000,
         vars=shape(2, 0, backref=0.001)),
    # Two shares below the sweep the TODO asked for, because 1% and 0.1% cannot bracket the thing the
    # sweep is for. p99 is taken over VALID transactions, and a batch that hits the failure path drags
    # its valid transactions through the retry with it -- so the share of BATCHES holding a conflict is
    # what decides the tail, and at width ~175 that share is 1-(1-p)^175: 83% at p=1%, 16% at 0.1%,
    # 1.7% at 0.01%, 0.18% at 0.001%. The bound is a 99th percentile, so it can absorb 1% of
    # transactions and no more, which puts the crossing at ~0.006% -- between the last two. Without
    # them the sweep returns three misses and locates nothing; with them it brackets the share at which
    # this tablet layout becomes usable, which is the number an operator actually needs.
    # Seeds are set from how much of the pipeline each share is expected to spend on retries.
    dict(id="9c-ds001", figure="9c", x=0.01, label="0.01% double spend", seed=200_000,
         vars=shape(2, 0, backref=0.0001)),
    dict(id="9c-ds0001", figure="9c", x=0.001, label="0.001% double spend", seed=480_000,
         vars=shape(2, 0, backref=0.00001)),

    # The claim the document needs and has never had: a 300-second hold at 8 tablets on a fresh
    # deployment. A probe met the bound there -- 181,031 offered, 172,150 and 172,112 committed in two
    # independent windows, 239 and 252 ms p99, 21% CPU -- but no hold has reproduced it, and the three that
    # followed inherited queues from probes above the knee. Seeded at 200,000 so the descent lands near
    # 170,000 rather than bottoming out at the 181,031 the default seed cannot go below.
    dict(id="9c-ds5-hold8", figure="conflict-tablets", x=8, label="5% double spend, 8 tablets, held",
         seed=200_000, vars=dict(shape(2, 0, backref=0.05),
                                 committer_database_table_pre_split_tablets=8)),

    # A ladder where capacity is high enough to bracket a knee. At 120 tablets capacity is under 25,000,
    # so every rung of the 5M ladder sat above it and measured the collapsed regime rather than a curve.
    # The same eight tablets with automatic splitting PINNED OFF, as a twin rather than a replacement:
    # `hold8` and `ladder8tab` keep the default policy so they stay comparable with the history, and this
    # one changes exactly one thing. It is the decisive test of the eight-tablet anomaly -- 172,260 tps at
    # 239 ms on a 90 s probe against six 300 s holds at ~30 s. Eight tablets over twelve tablet servers is
    # 0.67 per node, inside splitting's LOW phase where the threshold is 128 MiB rather than 10 GiB, and a
    # 90 s probe at that rate writes enough to cross it. So the probe plausibly measured 8 tablets while
    # the holds measured 8 growing to N, and cost rises with tablet count on every reading we have.
    #
    # If these rungs sustain near 172,000, the anomaly is explained and the claim changes from "eight
    # tablets is refuted" to "refuted at the default splitting policy" -- a materially different statement,
    # and one an operator can act on. If they still collapse, splitting was never the explanation and the
    # eight-tablet probe stands as unexplained. Ascending, so no rung inherits from the one before it.
    dict(id="9c-ds5-hold8-nosplitting", figure="conflict-tablets", x=8, mode="curve",
         label="5% double spend, 8 tablets, no auto-splitting",
         # Deliberately the SAME rate list as `9c-ds5-ladder8tab`, so the pair differs in the splitting
         # policy and nothing else. Mismatched rungs would leave the comparison arguing about interpolation.
         rates=[25_000, 50_000, 100_000, 150_000, 200_000, 250_000],
         vars=dict(shape(2, 0, backref=0.05),
                   committer_database_table_pre_split_tablets=8)),

    dict(id="9c-ds5-ladder8tab", figure="conflict-ladder", x=8, mode="curve",
         label="5% double spend, 8 tablets",
         rates=[25000, 50000, 100000, 150000, 200000, 250000],
         vars=dict(shape(2, 0, backref=0.05),
                   committer_database_table_pre_split_tablets=8)),

    # The tablet sweep, which turns a 9x observation into a threshold prediction.
    #
    # CPU per transaction on the busiest host is what separates a cost from a queueing artefact:
    # conflict-free runs 99 us/tx, 5% conflicts at 120 tablets runs 2,133 us/tx, and 5% conflicts at 8
    # tablets runs 75 us/tx -- no more than no conflicts at all. So the cost is real and it is tablet
    # dependent.
    #
    # `insert_ns` inserts the batch blind and returns on success, doing no lookup; only its
    # `unique_violation` handler runs `key = ANY(_keys)` over EVERY key in the batch. That is why a
    # conflict-free run never trips it and one conflicting key makes a whole batch pay. YugabyteDB
    # batches such a lookup per tablet only while tablets x keys stays under about 32,768, and a
    # 500-transaction chunk carries ~1,000 keys -- so the cliff should sit between 32 tablets (32,000,
    # just under) and 64 (64,000, over).
    #
    # Prediction: 8, 16 and 32 fast and near conflict-free CPU per transaction; 64 and 120 collapsed. If
    # 32 collapses too, the threshold constant is wrong on this version and needs re-measuring.
    # Points chosen for the width that actually reaches the insert, not for the graph's chunk. At ~304
    # keys an insert, tablets x keys reaches 32,768 near 108 tablets, so 8/16/32/64 all sit UNDER the
    # threshold and only 120 is over -- a sweep across those would likely show no crossing and be read as
    # refuting a constant that was never tested. These four bracket 108 from both sides. Each row records
    # tx_per_insert, so the result can be plotted against tablets x keys even if the width moves.
    dict(id="9c-ds5-tab64", figure="conflict-tablets", x=64, label="5% double spend, 64 tablets",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_database_table_pre_split_tablets=64)),
    dict(id="9c-ds5-tab88", figure="conflict-tablets", x=88, label="5% double spend, 88 tablets",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_database_table_pre_split_tablets=88)),
    dict(id="9c-ds5-tab96", figure="conflict-tablets", x=96, label="5% double spend, 96 tablets",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_database_table_pre_split_tablets=96)),
    dict(id="9c-ds5-tab160", figure="conflict-tablets", x=160, label="5% double spend, 160 tablets",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_database_table_pre_split_tablets=160)),
    dict(id="9c-ds5-sc200k", figure="conflict-why", x=200, label="5% double spend, 200k sidecar limit",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_sidecar_waiting_txs_limit=200_000)),

    # A ladder rather than a search, at the limit the run cannot reach. A search reports one number and
    # steps down from any failure, which is the wrong instrument for a cliff: what matters is the rate at
    # which the graph's population crosses its limit, and whether throughput falls off a step there or
    # bends like a knee. Every rung is a measurement, so the shape is visible either way.
    dict(id="9c-ds5-ladder5m", figure="conflict-ladder", x=5000, mode="curve",
         label="5% double spend, 5M graph limit",
         rates=[25000, 50000, 100000, 200000, 300000, 400000, 500000],
         vars=dict(shape(2, 0, backref=0.05),
                   committer_coordinator_dep_graph_wait_tx_limit=5_000_000)),
    # The same ladder at the limit in use, so the two curves can be read against each other. If the cliff
    # is real this one steps down where its population reaches 500,000 and the other does not.
    dict(id="9c-ds5-ladder500k", figure="conflict-ladder", x=500, mode="curve",
         label="5% double spend, 500k graph limit",
         rates=[25000, 50000, 100000, 200000, 300000, 400000, 500000],
         vars=shape(2, 0, backref=0.05)),
    # The tablet layout, laddered. The single-variable A/B already says this is the knob: 5% double
    # spend drains 20,235 tps on the default layout and 124,181-130,233 with eight pre-split tablets,
    # under the same 500,000 sidecar window, so no window explains the difference. What the A/B does
    # not give is an operating point -- both split8 holds were offered 155,000-181,000 and drained
    # while overloaded, at 26 s mean latency. This ladder finds the rate the split layout sustains
    # with latency that means something.
    dict(id="9c-ds5-split8-ladder", figure="conflict-ladder", x=8, mode="curve",
         label="5% double spend, 8 tablets",
         rates=[25000, 50000, 100000, 150000, 200000],
         vars=dict(shape(2, 0, backref=0.05),
                   committer_database_table_pre_split_tablets=8)),
    # A third point on the tablet axis, so the claim is a trend and not one lucky value of eight.
    dict(id="9c-ds5-split32", figure="conflict-why", x=32, label="5% double spend, 32 tablets",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_database_table_pre_split_tablets=32)),
    # 48 tablets, to bracket the bound rather than confirm the slope again. The failing lookup costs
    # ~13.8 ms per tablet regardless of how many keys it seeks (1.212 s at 88, 1.317 at 96, 1.709 at 120,
    # all within 3% of that constant while the width doubled), and it runs ~1.8 times per commit. So the
    # 1 s bound sits at tablets x 13.8 ms x 1.8 = 1000, i.e. ~40 tablets: 32 should MEET at 0.79 s per
    # commit and 48 should MISS at 1.19 s. That pair is the first conflicting operating point at a tablet
    # count anyone would run, which 64 and 160 cannot be -- both are predicted misses on a slope that
    # three points already fix.
    # The other axis of the conflict cost: keys, at a fixed tablet count. It answers whether the failing
    # lookup pays per tablet or per key, which the tablet sweep cannot, since a rate search cannot hold
    # the width still -- the width moves with backlog depth, so probes at unmatched distances above
    # capacity differ in overload as much as in shape. Hence fixed rates, like tabhold, and rates well
    # under this shape's capacity: four read-writes is twice the keys per transaction, so capacity is
    # near half of the ~20,400 the two-key shape sustains at this split, and 5,000/8,000 clear it.
    #
    # RUN IT AFTER tabhold, not before. Its premise is that a tablet law exists to be attributed to
    # keys instead, and tabhold is what establishes whether there is one: the per-tablet fit is
    # currently withdrawn, not confirmed. With no law on the tablet axis this point has nothing to
    # separate. Two rungs so the rows can be shown uncontaminated rather than asserted to be.
    dict(id="9c-ds5-rw4", figure="conflict-keys", x=120, mode="curve", rates=[5_000, 8_000],
         label="5% double spend, 4 read-writes", vars=shape(4, 0, backref=0.05)),
    dict(id="9c-ds5-tab48", figure="conflict-why", x=48, label="5% double spend, 48 tablets",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05),
                                   committer_database_table_pre_split_tablets=48)),
    # The tablet axis, measured at a FIXED rate instead of by rate search -- because the rate searches
    # cannot answer it. `db_insert` under conflicts is not a service time: at 96 tablets it reads 2.05 s
    # in a saturated window and 1.33 s while draining, same deployment, same tablet count. The width moves
    # with backlog depth too (a deeper queue makes the batcher pick up more per cycle, which is why 96
    # saturated came out twice as wide as 88 at a LOWER throughput). So two probes sitting at unmatched
    # distances above capacity differ in overload depth as well as tablets, and every cost law fitted to
    # that mix has come out differently: across eight rows, per-tablet spreads 80%, per-key 46%,
    # per-key-x-tablet 31%. The apparent 13.8 ms per tablet was three rows that happened to share a
    # 300-350 key width, one of which was a drain window.
    #
    # 15,000 tps clears every capacity in the sweep -- 20,000 at 120 and 96, 27,400 at 88, more below --
    # so the backlog stays near zero and the tablet count is the only difference. Each point carries a
    # second rung at 10,000: one fixed rate cannot show that it is uncontaminated, two can, because a cost
    # that is a service time gives the same answer at both and a queueing artefact does not. Rows record
    # `inflight_growth`, so fit only where it is ~0 -- with the caveat that growth alone is not enough,
    # since a queue pinned at its ceiling also has zero derivative (the tab88 rows at 8.6x capacity read
    # -1,111/s). Offered-against-retired plus mean latency is the gate.
    #
    # The per-point validity test, which is why there are two rungs and not one: `db_insert x attempts` is
    # the commit's SERVICE time only if it is the same at both rates, because 10,000 sits at about half
    # capacity and 15,000 at three quarters, so a queueing term cannot be equal at both. If a point's two
    # rungs agree, that point's number can be quoted; if they move, the rate is still too high for it.
    # Established already at 96 tablets, from the one probe that ran at grow exactly 0 (offered 15,659,
    # finished 15,636, 77% of capacity): insert 1.764 s x 1.90 attempts = 3.35 s of service against a
    # 6.94 s mean, so 3.35 s service and 3.59 s queue. 120 tablets agrees at 1.709 x 1.91 = 3.26 s. The
    # bound therefore fails on service alone by more than 3x at both, which is a measurement rather than a
    # derivation. At 32 tablets a pass needs the service term under 1 s, i.e. insert under ~0.53 s, so 32
    # is the first point on the axis where the answer could be yes -- and it is listed first.
    # The no-split ceiling, because the ladder below will end unbracketed. Rung 2 retired 28,537 tps at
    # 192 ms with the busiest host at 3% CPU and the insert at 13.9 ms, so nothing is near a limit and the
    # rungs at 60,000 and 100,000 should pass too. The conflict-free ceiling on this arm is 518,000: if the
    # conflicting no-split ceiling lands anywhere near it, the 120-way pre-split is buying nothing for this
    # workload on either axis, which is a much stronger statement than "no split is better at 30,000".
    # Splitting is pinned off and the count set explicitly to the 23 the no-split table settles at, so this
    # measures a ceiling at a FIXED layout rather than one that could drift under a higher write rate.
    dict(id="9c-nosplit-ds5-hi", figure="conflict-nosplit", x=5, mode="curve",
         label="5% double spend, 23 tablets, ceiling",
         # Pinned at 12, not 23: the master's `tablets` array includes `Deleted` split parents alongside
         # the `Running` children, so a total-entry count overstates the live layout. Read by state on a
         # fresh table: 2 Running + 1 Deleted for one completed split. 12 running is what the master
         # reports when the array is filtered by state, and what `yb-admin list_tablets` reports when
         # passed 0 -- two independent readings, which is why the pin is 12.
         #
         # An earlier revision of this comment derived 12 from the 23 totals via `total = 2*running - 1`.
         # That formula is withdrawn: it assumes every split is complete and every parent still retained,
         # neither of which holds generally, and it agreed with the reading here by coincidence of a
         # particular history. The pin does not depend on it -- both direct readings stand on their own.
         # 12 is where the LOW phase ends
         # (1.0 per tablet server), not where splitting ends: above it the threshold rises to 10 GiB per
         # tablet and the count can keep going to 24 per server, i.e. 288. So the table pauses at 12 until
         # it holds ~120 GiB, and a no-split table has the same DESTINATION as one created with 120 -- a
         # table created with 120 was found holding 288 after eleven hours -- it merely starts further
         # away. That is why the ageing test below exists rather than being assumed away.
         # `SPLIT INTO N` creates N running tablets, so 12 is what reproduces the measured layout.
         # Top rung is 350,000, not 400,000, because the load generator's own ceiling on this arm is
         # ~400,000 without the deep buffer -- a miss at 400,000 could be the generator rather than the
         # committer, and an ambiguous top rung brackets nothing. If all three pass, the conflicting
         # ceiling is >=350,000 against 518,000 conflict-free, which is already the strong statement.
         rates=[150_000, 250_000, 350_000],
         vars=dict(shape(2, 0, backref=0.05),
                   committer_database_table_pre_split_tablets=12)),

    # 250,000 read pass on one deployment and fail on the next, and the difference is not marginal: the
    # insert was 17.0 ms in the rung that met and 262 ms in the repeat, at the same rate and the same
    # rendered shape, with mvcc resetting 10.5M -> 3.2M across the redeploy so the second really was a
    # fresh table rather than an undrained one. The ladder's own bridge rung is what caught it.
    #
    # One rung cannot say which reading is the workload and which is the deployment, and the last time a
    # single-rung anomaly at this layout was argued rather than repeated, three sessions reversed on it five
    # times in ninety minutes and one repeat settled it in twenty-five minutes. So: three separate batches,
    # each of which begins with its own bring-up, giving three independent fresh-deployment readings of the
    # one rate in question. Identical vars to `9c-nosplit-ds5-hi` so the readings pool with its rungs.
    #
    # This has to run BEFORE the insert_ns swap. It is a measurement of the failure path the rewrite
    # removes, and CREATE OR REPLACE is not a live upgrade path, so after the swap it cannot be taken at all.
    dict(id="9c-nosplit250-rep1", figure="conflict-nosplit250", x=1, mode="curve",
         label="5% double spend, 12 tablets, 250k repeat 1", rates=[250_000],
         vars=dict(shape(2, 0, backref=0.05), committer_database_table_pre_split_tablets=12)),
    dict(id="9c-nosplit250-rep2", figure="conflict-nosplit250", x=2, mode="curve",
         label="5% double spend, 12 tablets, 250k repeat 2", rates=[250_000],
         vars=dict(shape(2, 0, backref=0.05), committer_database_table_pre_split_tablets=12)),
    dict(id="9c-nosplit250-rep3", figure="conflict-nosplit250", x=3, mode="curve",
         label="5% double spend, 12 tablets, 250k repeat 3", rates=[250_000],
         vars=dict(shape(2, 0, backref=0.05), committer_database_table_pre_split_tablets=12)),

    # Does the dependency graph's admission cap participate in the slow regime at twelve tablets?
    #
    # 250,000 at this layout has two stable operating points, reproduced twice each: one retiring the
    # rate at a 26 ms commit with the graph at 16,000-19,000, and one retiring ~171,000 at a 270 ms
    # commit with the graph pinned at exactly its 500,000 limit. Attempts per commit is 1.0 in both and
    # the slow run's batches are NARROWER, so neither the retry path nor a fan-out threshold explains it.
    #
    # The existing 20,000,000 test ruled the cap out AT THE 120-WAY PRE-SPLIT, where capacity is ~20,300
    # and the insert costs 1.7 s -- there the graph is full because in-flight equals throughput times
    # latency, an effect. It says nothing about this layout. Three repeats, because one reading cannot
    # distinguish a cap that participates from a coin flip that happened to land the same way: if all
    # three retire 250,000 the cap is part of the collapse, and if they still split it is not.
    #
    # Identical to 9c-nosplit250-rep* except for the limit, so the pair is a one-variable comparison.
    *[dict(id=f"9c-nosplit250-cap-rep{i}", figure="conflict-nosplit250cap", x=i, mode="curve",
           label=f"5% double spend, 12 tablets, 250k, 40x graph limit, repeat {i}", rates=[250_000],
           vars=dict(shape(2, 0, backref=0.05),
                     committer_database_table_pre_split_tablets=12,
                     committer_coordinator_dep_graph_wait_tx_limit=20_000_000))
      for i in (1, 2, 3)],

    # The A/B for `insert_ns`'s rewrite: `ON CONFLICT (key) DO NOTHING ... RETURNING key` in place of the
    # `EXCEPTION WHEN unique_violation` handler, with the violating set computed in the same statement as
    # `_keys EXCEPT ALL inserted`. No Go change and no contract change -- `insertStates` still consumes a
    # violating-key array and `commit()` already aborts on a non-empty result.
    #
    # IDs carry `-onconflict` deliberately, because a row does not record which binary produced it. Reusing
    # `9c-ds5` would put old-code and new-code rows under one id in the same results file, where
    # `best_per_x` pools by `config_key` and would silently mix them -- and the vars are identical, so
    # nothing would distinguish them but the timestamp.
    #
    # This is the measurement that can retire five queued batches. `tabhold`, the eight-tablet anomaly,
    # `ladderlow` and the conflict-share sweep all exist to characterise a failure path whose cost this is
    # meant to remove: if 5% double spends meet the bound at the 120-way split, none of them has a subject
    # left. So it runs before them -- and after the two share re-runs, which must finish on the CURRENT
    # binary or the panel's four shares span two code versions.
    #
    # A ladder rather than a search, because if the rewrite works the capacity is unknown: the old code
    # retires 20,300 and the conflict-free ceiling on this arm is 518,000, so the rungs span both.
    dict(id="9c-ds5-onconflict", figure="conflict-fix", x=5, mode="curve",
         label="5% double spend, 120 tablets, ON CONFLICT",
         rates=[20_000, 50_000, 100_000, 200_000, 400_000],
         vars=dict(shape(2, 0, backref=0.05),
                   committer_database_table_pre_split_tablets=120)),
    # The regression side, and the one place the rewrite could cost something: the common path now
    # materialises a RETURNING set and compares cardinalities where it returned '{}' after a bare INSERT,
    # at ~3,400 calls a second per validator-committer. Read `db_commit` as well as `db_insert` here --
    # `db_insert` wraps `insertStates` only, so a cost landing in the surrounding transaction would show
    # in one and not the other.
    dict(id="9c-ds0-onconflict", figure="conflict-fix", x=0, mode="curve",
         label="0% double spend, 120 tablets, ON CONFLICT",
         rates=[500_000],
         vars=dict(shape(2, 0),
                   committer_database_table_pre_split_tablets=120)),

    # Does the no-split advantage survive the table AGEING? This decides whether the result is a deployment
    # recommendation or a property of a young table, and it is the one claim here a reader would act on.
    #
    # Two design errors were in the first version of this pair, both of which would have produced a
    # confident null. First, they were pinned with splitting DISABLED -- which makes the test impossible by
    # construction, since what is being tested is splitting resuming. Second, they could not reach the
    # threshold: `ns_0` grows 0.049 GB per tablet per minute at 56,000 tps, so 0.875 GB per tablet per
    # million transactions, and the 10 GiB high-phase threshold is 9.6 GB per tablet away from fresh. Three
    # 300 s rungs averaging 250,000 tps is fifteen minutes and reaches ~3.3 GB per tablet -- a third of the
    # way. It would have read "still 14 ms" and meant "no split happened", which is the same false
    # reassurance the SKIP_DEPLOY guard exists to prevent, one layer up.
    #
    # So: splitting LEFT ON, the count pre-split to the 12 the no-split table settles at, and a soak long
    # enough to cross. At 350,000 tps the crossing is ~31 minutes from fresh, so the soak runs a single long
    # hold (FX_HOLD in the chain) rather than a ladder, and `tablets.log` records the running count every
    # 30 s so the crossing is observed rather than assumed. If the soak misses its rate the row says so and
    # the aged measurement is void -- which is visible, unlike a table that quietly never split.
    #
    # The soak is 45 minutes and EXTENDED IF NEEDED rather than padded, because the binding constraint is
    # disk and not time. The crossing needs ~115 GB of state, but the ledger written to get there is larger:
    # 651M transactions at ~262 B is ~170 GB against 514 GB free, which fits -- while padding the hold to
    # 125 minutes "to be safe" would write 2.6B transactions and ~690 GB, and the run would die on the disk
    # floor having proved nothing. So: run 45 minutes, read the count, and run a second soak if it is still
    # 12. The highest rate ever sustained at this layout is 95,122 tps (ds5 rung 4, at 11% CPU with the
    # ladder out of rungs), so 350,000 is a 3.7x extrapolation -- plausible on that headroom, not proven,
    # and the crossing needs ~245,000 retired to happen inside 45 minutes.
    dict(id="9c-nosplit-ds5-soak", figure="conflict-nosplit", x=5, mode="curve",
         label="5% double spend, 12 tablets, soak to the split threshold",
         rates=[350_000],
         vars=dict(shape(2, 0, backref=0.05),
                   committer_database_table_pre_split_tablets=12)),
    # Then the same rate as `9c-nosplit-ds5-hi`'s first rung, on the soaked deployment, with FX_SKIP_DEPLOY.
    # Same rate at two table ages, so rate sensitivity cannot be mistaken for ageing. Compare against
    # ds5-hi's own 150,000 row and against the count in `tablets.log` at each.
    dict(id="9c-nosplit-ds5-age", figure="conflict-nosplit", x=5, mode="curve",
         label="5% double spend, aged table past the split threshold",
         rates=[150_000],
         vars=dict(shape(2, 0, backref=0.05),
                   committer_database_table_pre_split_tablets=12)),

    # Conflicts with pre-splitting DISABLED, held below capacity -- the one configuration where a
    # conflicting workload meets the bound, and the section's only positive result. Already measured as
    # probes and never confirmed: `split0-ds10` climbed 17 steps from 30,000 to 102,727 offered with
    # `inflight_growth` 0 at every rung, finished equal to offered, and a p99 pinned at 195-197 ms the
    # whole way, committing 92,958 at the top; `split0-ds30` does the same to 76,352 at 192-195 ms. The
    # climb ran out of UP_STEPS while still passing, so those are lower bounds and no ceiling was found.
    #
    # No hold has ever been attempted BELOW capacity on this series, which is why none confirms it: the
    # driver holds at the last passing probe, which here was the top of the climb, so hold 1 failed at
    # 102,727 and holds 2 and 3 inherited its backlog and read `finished` above `offered` -- draining, not
    # measuring. ds30's one "met" hold has growth -12,433/s and is a drain artefact too.
    #
    # So these are fixed-rate ladders starting well under the known-passing probe: rung 1 is the
    # confirmation the figure needs, and the rungs above it look for the ceiling the search never reached.
    # Ascending, so a rung cannot inherit from the one before it. 5% is included because the 120-tablet
    # series is 5% -- without it the comparison would cross two conflict shares as well as two layouts.
    # These rows also all predate `fast_block_prepare`, so they need re-running before joining today's axis.
    *[dict(id=f"9c-nosplit-ds{int(share * 100)}", figure="conflict-nosplit", x=share * 100, mode="curve",
           label=f"{share:.0%} double spend, no pre-split", rates=rates,
           vars=dict(shape(2, 0, backref=share),
                     committer_database_table_pre_split_tablets=0))
      # Rungs start BELOW the 120-way split's 20,389 tps rather than near the old no-split figures,
      # because those figures are not a prediction for this workload: `split0-ds10/20/30` ran at
      # `loadgen_tx_reference_gap: 0` (the inventory default, predating the gap logic), so their
      # references named keys whose creating transaction had not committed. The coordinator has to ORDER
      # those -- a convoy -- rather than reject them, which is the measurement this file's own comment
      # calls "a convoy instead of double spends". So 92,958 tps at 195 ms was the serialization path and
      # says nothing about the insert failure path. These runs use gap 300,000 via shape(), which makes
      # them the first real no-pre-split double-spend measurement and leaves their capacity genuinely
      # unknown -- so the ladder brackets from under the worst known layout up to the convoy figures,
      # instead of assuming the answer is near the top.
      # All four shares share ONE rate list, which they did not at first and had to. `throughput()` counts
      # transactions FINISHED -- committed plus rejected, the paper's own convention, since its Figure 9b
      # shows throughput rising with the invalid share. So a panel series plots the top rung each share
      # reached, and with 5%/10% topping out at 100,000 while 20%/30% topped out at 80,000 the series would
      # have sloped down above 10% purely because of the ladder, not the pipeline. Every share has met every
      # rung offered at ~190 ms and 10% CPU, so the decline would have been an artefact presented as a
      # finding -- and the caption "highest rate tried, not a ceiling" invites exactly the question it
      # cannot answer. One list, so a difference between shares is a difference in the system -- with one
      # deliberate exception at 30%, noted below.
      # 30% carries one extra INTERIOR rung at 25,000: a direct repeat of the only failing measurement in
      # the whole no-split series, where the tail went to 3,565 ms while the rate arrived in full, the queue
      # stayed flat, CPU was 3%, the median IMPROVED to 130 ms, and the insert tripled to 52.9 ms at
      # constant attempts and constant width. One measurement is thin to hang a section's caveat on. It
      # cannot distort the panel, because `best_per_x` reports the highest rung that MET and this one sits
      # between two rungs already in the list -- so it can only ever sharpen where the knee is.
      for share, rates in ((0.05, [15_000, 30_000, 60_000, 100_000]),
                           (0.10, [15_000, 30_000, 60_000, 100_000]),
                           (0.20, [15_000, 30_000, 60_000, 100_000]),
                           (0.30, [15_000, 25_000, 30_000, 60_000, 100_000]))],
    #
    # Automatic tablet splitting must be pinned OFF for this batch, and that is done by running it against
    # `inventory/cluster-nosplitting.yaml` rather than by a var here. A `yugabyte_master_extra_flags` in an
    # experiment's vars is a SILENT NO-OP: a per-experiment redeploy tears down
    # `fabric_x_committers:load_generators` and nothing else, so the master is never restarted and never
    # reads the flag. Only a batch bring-up restarts the database, and it reads the inventory. This was set
    # here for two hours and would have produced a tablet sweep whose layout drifted under it while the
    # comments claimed otherwise -- the same shape of defect as the search seed and the ageing pin.
    # Otherwise the one variable this batch exists to fix is not fixed. It is on by default (read from the running master: `enable_automatic_tablet
    # _splitting = true`), and it is not hypothetical -- with `pre_split_tablets: 0` the state table was
    # observed going 15 -> 19 -> 23 tablets in two minutes at 15,000 tps, while its SST files grew 743 MB
    # -> 1.19 GB. Splitting triggers on tablet SIZE in phases set by tablets per node: with twelve tablet
    # servers the low phase holds up to twelve tablets at a 128 MiB threshold, the high phase up to 288 at
    # 10 GiB. So a 300 s hold at these rates crosses the low threshold easily, and the configurations that
    # drift are exactly the low ones -- which is also the parsimonious explanation of the eight-tablet
    # anomaly: 239 ms on a 90 s probe and ~30 s on six 300 s holds is what measuring 8 tablets and then
    # 8-growing-to-N would look like, given that cost rises with tablet count on every reading we have.
    *[dict(id=f"9c-ds5-tabhold{t}", figure="conflict-tabhold", x=t, mode="curve",
           label=f"5% double spend, {t} tablets, fixed rate", rates=[10_000, 15_000],
           vars=dict(shape(2, 0, backref=0.05),
                     committer_database_table_pre_split_tablets=t))
      # Retargeted into the interval that is actually unprobed. The no-split layout settles at 23 tablets
      # and its insert is 15.2 ms; 88 tablets costs 1,196 ms. That is a 3.8x change in layout for a 79x
      # change in cost, so a linear per-tablet law under-predicts by twenty-one and is refuted the same way
      # per-key was. What is left is a CLIFF somewhere between 23 and 88, which nothing has measured.
      # 88, 96 and 120 only re-measure the flat slow side -- their service time is 2.3-3.4 s, so no rate
      # can meet the bound there and `ladderlow` already covers 120 sub-capacity. 23 is included as the
      # control for the headline result: it separates "23 tablets" from "no SPLIT INTO clause", which the
      # no-split run cannot distinguish because splitting produced its 23 rather than the DDL.
      # 12 is the control -- the running count the no-split table settles at -- and 24/48/64 probe the
      # interval above it. The insert is insensitive to the count from 1 up to 12 (it FELL 15.2 -> 13.6 ms
      # while the count grew and the rate rose 6.7x), and costs 1,196 ms at 88, so the discontinuity is
      # between 12 and 88 and these four span it. 88/96/120 only re-measure the flat slow side.
      for t in (12, 24, 48, 64)],
    # And the published topology: nine validator-committers on the nine database nodes that carry no
    # master, against the six here. Tests whether the tier width is part of it independently.
    dict(id="9c-ds5-vc9", figure="conflict-why", x=9, label="5% double spend, 9 validator-committers",
         seed=BASE_SEED, vars=dict(shape(2, 0, backref=0.05))),
    # The throughput-against-latency curve, in place of the paper's failure figure: a rate ladder rather
    # than a search, so there is no gate in it and every point after the first arrives warm. It is the
    # figure this evaluation was asked for and the one that exposed the block-size finding.
    dict(id="curve", figure="curve", x=0, label="2 read-writes", mode="curve",
         rates=[10000, 25000, 50000, 100000, 200000, 300000, 400000,
                460000, 500000, 530000, 560000],
         vars=shape(2, 0)),

    # The latency end of the comparison, as a second ladder rather than a knee. The main curve shows
    # p50 flat at 125-127 ms from 10,000 to 100,000 tps: a structural floor, not queueing, and the
    # paper's 85 ms p99 at 419,000 tps sits BELOW this deployment's floor at 10,000. The suspect is
    # the block. Nothing in a block moves until the block is cut, so a transaction waits for its
    # block to fill and then for one whole-block traversal at every block-granular stage -- the
    # sidecar's ledger append, the relay's mapping pass, the coordinator's batching.
    #
    # 500-transaction blocks should cut that floor by roughly twenty times and cost throughput for
    # the same reason it was raised to 10,000 in the first place: the sidecar's serialized append
    # capped near 110,000 tps at that block size. Two ladders on one pair of axes is the trade-off
    # rather than a single point of it, which is what a throughput-against-latency figure is for. The
    # ladder stops at 140,000 because the ceiling is expected an order of magnitude below the main
    # curve's.
    dict(id="curve500", figure="curve500", x=500, label="500-tx blocks", mode="curve",
         rates=[10000, 25000, 50000, 75000, 100000, 120000, 140000],
         vars=shape(2, 0, block=500)),

    # The paper attributes its 41% drop from 1/1 to 4/4 to the mutex protecting the coordinator's
    # dependency graph. This deployment does not run that graph -- it runs the simple manager, one
    # map under a single owning goroutine, because that mutex was the ceiling at ~375,000 tps -- and
    # the selector for it exists only on this evaluation branch, so these two points are also the
    # only ones here that measure a coordinator upstream can be configured into. A stage benchmark
    # puts the two managers 1.6x apart at one key and 1.16x at four, which these can falsify.
    dict(id="gdg-rw1", figure="graph", x=1, label="1 read-write, global graph", seed=400000,
         vars=dict(shape(1, 0),
                   committer_coordinator_dep_graph_use_simple_manager=False)),
    dict(id="gdg-rw4", figure="graph", x=4, label="4 read-writes, global graph", seed=300000,
         vars=dict(shape(4, 0),
                   committer_coordinator_dep_graph_use_simple_manager=False)),

    # Where small blocks actually stop. The 500-transaction ladder was capped at 140,000 on the
    # expectation that the sidecar's serialized ledger append would plateau near 110,000 -- the figure
    # measured before the block-store index fix. It does not: at 100,000 tps the append path is at 4.6%
    # utilisation and 0.23 ms per block, which would not saturate until roughly 4,300 blocks a second.
    # So the old ceiling is either gone or was never the append. This extension finds the real one, and
    # if small blocks reach the large-block rates then the 10,000-transaction default is costing 590 ms
    # of latency for nothing.
    dict(id="curve500hi", figure="curve500", x=500, label="500-tx blocks", mode="curve",
         rates=[170000, 200000, 240000, 280000, 330000, 380000],
         vars=shape(2, 0, block=500)),

    # Above 380,000 the ladder has never been run since the generator's block preparation was fixed.
    # What it measured before was the generator: at 450,000 offered it SENT 429,355 and the committer
    # finished 428,945 of that, 99.9% of what arrived, so the 430,000 plateau was production and not
    # commitment. Preparation deep-cloned and hashed every block in one goroutine, 608 us per block at
    # this size; `fast-block-prepare` takes it to 8.4 us. These rungs are what says whether the
    # committer's own small-block ceiling is above the old one -- they need a loadgen built from a
    # branch containing that change, with `fast-block-prepare` and `prepare-in-place` both on.
    dict(id="curve500top", figure="curve500", x=500, label="500-tx blocks", mode="curve",
         rates=[430000, 480000, 530000, 580000],
         vars=shape(2, 0, block=500)),

    # What actually caps the generator at 500 transactions a block, since block preparation does not.
    # With `fast-block-prepare` on, preparation is 1,133x cheaper and the ladder did not move: 430,000
    # to 580,000 offered all SENT about 400,000, with the generator at 20% CPU. Blocked, not busy.
    #
    # The suspect is the buffer between the workload and the sidecar, which is counted in BLOCKS:
    # `out-block-capacity: 100` is 1,000,000 transactions at 10,000 a block and only 50,000 at 500 --
    # twenty times less headroom for the same transaction rate. A generator that fills it stalls on the
    # channel, which is what 20% CPU while failing to send looks like. This ladder gives the small-block
    # buffer the same transaction depth the large-block one has.
    dict(id="curve500buf", figure="curve500buf", x=500, label="500-tx blocks, 2,000-block buffer",
         mode="curve", rates=[330000, 380000, 430000, 480000, 530000],
         vars=dict(shape(2, 0, block=500), loadgen_mock_orderer_out_block_capacity=2000)),

    # Whether the tablet split costs anything on the conflict workload, where read validation looks
    # up keys that exist rather than keys that do not. The zero-conflict point is the control: if
    # the default split is slower there and faster at 10%, the split is a workload-dependent trade
    # rather than a misconfiguration, and the recommendation has to be conditional.
    dict(id="split0-ds0", figure="split", x=0, label="0% double spend, default split",
         seed=BASE_SEED,
         vars=dict(shape(2, 0), committer_database_table_pre_split_tablets=0)),
    dict(id="split0-ds10", figure="split", x=10, label="10% double spend, default split",
         seed=30000,
         vars=dict(shape(2, 0, backref=0.10), committer_database_table_pre_split_tablets=0)),
    # The rest of the double-spend series, at the split where a double spend is measurable at all. The
    # 9c panel has one bar and three "no rate qualified": at the 120-way pre-split every conflict point
    # collapses to tens of seconds and no rate meets the conditions. At the default split 10% held
    # cleanly at 41,273 tps, so these two turn 9c into a series -- lower in absolute terms than the
    # panel's 0% bar, which keeps the 120-way split, and the split has to be stated with them.
    dict(id="split0-ds20", figure="split", x=20, label="20% double spend, default split",
         seed=30000,
         vars=dict(shape(2, 0, backref=0.20), committer_database_table_pre_split_tablets=0)),
    dict(id="split0-ds30", figure="split", x=30, label="30% double spend, default split",
         seed=30000,
         vars=dict(shape(2, 0, backref=0.30), committer_database_table_pre_split_tablets=0)),

    # The shippable side of the same threshold: it constrains the PAIR (tablets x keys per lookup),
    # so narrowing the lookup preserves batching as well as reducing tablets does, and unlike
    # reducing tablets it keeps the write concurrency the 120-way split was chosen for. chunk-size
    # bounds the transactions per dependency-graph chunk and so, downstream, the keys per lookup.
    # A diagnostic, not a sweep point: chunk-size also changes the graph's release granularity and
    # the pipeline's in-flight profile.
    dict(id="chunk-rw4", figure="chunk", x=64, label="4 read-writes, 64-tx chunks", seed=300000,
         vars=dict(shape(4, 0), committer_coordinator_dep_graph_chunk_size=64)),

    # What creating output keys costs. A blind write is an output at a new key and the validator
    # resolves its version itself (populateVersionsAndCategorizeBlindWrites), which is a lookup per
    # output key inside the commit path. n read-writes plus n blind writes touches 2n keys, which is
    # the paper's UTXO shape read literally.
    #
    # Seeded from the measured brackets, not from the read-write knees. The first attempt seeded these
    # at 300,000 and 200,000, where the search's six 15% steps bottom out at 113,145 -- above a shape
    # that delivers ~69,000 -- so it spent both points' attempts over-driven and exhausted without ever
    # offering a rate the shape could meet. The over-driven probes are what bracket it: 300,000,
    # 255,000 and 216,750 offered all returned 68,900-70,000 finished.
    # Ladders, not searches. A search looks for the highest rate that meets the conditions, and this
    # shape meets them at no rate: it delivers every rate offered with a flat queue -- 40,000 of 39,933,
    # 55,273 of 55,271 -- while the 99th percentile sits at a constant ~2,990 ms, at 34,000 tps with the
    # busiest machine at 29% CPU. That is a latency floor, roughly three seconds of it, and not
    # saturation, so the answer to what output creation costs is a floor rather than a knee and the
    # search can only ever report "no rate met the conditions" after spending six deployments finding
    # out. The rates bracket what the searches did measure: about 70,000 tps for one output and about
    # 6,500 for four.
    dict(id="9a-utxo1", figure="9a-utxo", x=1, label="1 in / 1 out", mode="curve",
         rates=[10000, 20000, 35000, 50000, 65000], vars=shape(1, 1)),
    dict(id="9a-utxo4", figure="9a-utxo", x=4, label="4 in / 4 out", mode="curve",
         rates=[1000, 2000, 4000, 6000, 8000], vars=shape(4, 4)),

    # One rung of the invalid-signature panel is unresolved and three probes would settle it. At
    # 559,872 the 0% configuration held twice on two deployments while 10% and 20% each failed once,
    # which hints that invalid signatures cost a rung rather than buying one -- the opposite of the
    # paper's +10%. Two attempts per configuration cannot separate that from the gate's noise, so
    # these three re-probe that one rate, one deployment each, no holds. Run with FX_HOLD=90 and
    # only="rung": curve mode measures each rate once and takes no confirmation.
    dict(id="rung-inv0", figure="rung", x=0, label="0% invalid at 559,872", mode="curve",
         rates=[559872], vars=shape(2, 0, invalid=0.0)),
    dict(id="rung-inv10", figure="rung", x=10, label="10% invalid at 559,872", mode="curve",
         rates=[559872], vars=shape(2, 0, invalid=0.1)),
    dict(id="rung-inv20", figure="rung", x=20, label="20% invalid at 559,872", mode="curve",
         rates=[559872], vars=shape(2, 0, invalid=0.2)),
]

# The end-to-end matrix, for the arm that has a real Arma ordering service in the path. Selected with
# FX_MATRIX=e2e, and kept separate rather than merged into the list above because every seed here is
# wrong for the committer-only arm and every seed above is wrong for this one.
#
# The paper has no end-to-end figure to recreate. Its Section 6.2 measures ordering alone -- 430,000 tps
# at this arm's 4 parties and 4 shards, reading Figure 7a, and 414,000 at 2 shards -- and its
# Section 6.3 measures the committer alone with a mock orderer, which is the arm the matrix above ran
# on. So these points are not a recreation of a published number; they price what putting real ordering
# in the path costs, against two published ceilings that bracket it.
#
# This arm reports one figure, the latency-throughput curve, so it runs one ladder and no knee searches.
# A ladder is the right instrument for it anyway: this arm's ceiling has never been measured -- the only
# load it has carried is a 2,000 tps soak -- and a ladder finds where the curve turns up without needing
# a seed anywhere near the answer.
#
# One knob does not carry over. On this arm the batchers cut the blocks, not the generator, so
# `loadgen_block_max_size` does nothing and there is no block-size ladder here; the observed batch is
# about 489 transactions, which is already near the 500 the committer-only arm found best.
E2E_EXPERIMENTS = [
    # Step 1: find the best-throughput configuration, before spending ladders on it. Both of these are
    # deployment shapes rather than workloads, so each needs its own bring-up -- they are listed here to
    # document the sequence, and are selected one at a time with an id filter.
    #
    #   4 shards, one volume per co-located shard: the batchers already sit two to a machine, and until now
    #   both wrote to /data1 while /data2 idled. `orderer_data_dir` is per inventory host and each batcher
    #   process is its own inventory host, so the second shard on each machine now writes to the second
    #   volume. Run with inventory/cluster-orderer.yaml.
    #
    #   8 shards, four to a machine: the per-shard ceiling was ~158,000 tps while batcher processes sat at
    #   15-20% of a 32-core box, so more shards per machine is the cheapest way to buy throughput if the
    #   constraint really is per shard. Run with inventory/cluster-orderer-8shard.yaml.
    #
    # A ladder rather than a knee search for both: what is wanted is where each configuration turns up, and
    # a search on this arm cannot redeploy per point without breaking the CA.
    dict(id="e2e-shape-4s", figure="shape", x=4, label="4 shards, one volume each", mode="curve",
         rates=[25000, 50000, 100000, 150000, 200000, 250000, 300000], vars=shape(2, 0)),
    dict(id="e2e-shape-8s", figure="shape", x=8, label="8 shards, two per volume", mode="curve",
         rates=[25000, 50000, 100000, 150000, 200000, 250000, 300000, 400000], vars=shape(2, 0)),

    # Both ladders topped out at their own highest rung rather than at a knee -- four shards held
    # 300,000 tps with the routers at 65% CPU -- so the ceiling is above the range first measured
    # and the two configurations cannot be compared over different ranges. These extend each ladder
    # upward. They carry the same `label` as the ladder they extend, which is what joins them into
    # one series, and a separate id so the driver does not treat the original as needing a re-run.
    dict(id="e2e-shape-4s-hi", figure="shape", x=4, label="4 shards, one volume each", mode="curve",
         rates=[350000, 400000, 450000, 500000, 550000], vars=shape(2, 0)),
    dict(id="e2e-shape-8s-hi", figure="shape", x=8, label="8 shards, two per volume", mode="curve",
         rates=[450000, 500000, 550000, 600000], vars=shape(2, 0)),

    # The discriminator for the fill confound. Both ladders above climb on ONE deployment, so rate and
    # table fill rise together: over the eight-shard ladder the database went from 9.4M rows to 1.31
    # billion and the disk from 513 GB free to 62, while db_commit latency went 18 ms to 232. Its knee
    # at 500,000 tps is therefore as consistent with a full database as with the offered rate, and the
    # four-shard extension is worse off still -- it began after a reset, so its top rungs face a
    # fraction of the fill the eight-shard ones did and the two cannot be compared there at all.
    #
    # These re-measure only the top of the range, each on a fresh deployment, so fill is low and
    # matched. Same rates for both shapes. If a rate that missed at high fill holds at low fill, the
    # ceiling was fill; if it misses either way, the ceiling is the rate.
    #
    # Separate ids and labels, so they form their own series rather than merging into the ladders whose
    # confound they exist to test.
    dict(id="e2e-fresh-4s", figure="shape", x=4, mode="curve",
         label="4 shards, fresh database", rates=[450000, 500000, 550000], vars=shape(2, 0)),
    dict(id="e2e-fresh-8s", figure="shape", x=8, mode="curve",
         label="8 shards, fresh database", rates=[450000, 500000, 550000], vars=shape(2, 0)),

    # Step 2: the latency-throughput curve at two block sizes, on whichever shape won. On this arm the
    # batchers cut the blocks, so the knob is the shared config's Batching.BatchSize.MaxMessageCount, set
    # through `armageddon_batch_max_message_count` -- which means a block size change needs the shared
    # config regenerated, not a loadgen file re-rendered. One deployment per ladder.
    #
    # The rates run down to 10,000 deliberately. `BatchCreationTimeout` is 500 ms and hardcoded in the
    # shared-config template, so at low rates a batch is cut by that timer rather than by size, and the
    # floor it puts under latency is the whole point of comparing the two block sizes -- the same shape as
    # the committer arm's finding that a 10,000-transaction block costs ~590 ms at low rates because
    # nothing in a block moves until the block is cut. Measuring only near the knee would hide it.
    #
    # The batch size is in the label because that is what the figure's legend reads.
    dict(id="e2e-curve-large", figure="curve", x=10000, label="10,000-transaction batches",
         mode="curve", rates=[10000, 25000, 50000, 100000, 150000, 200000, 250000, 300000],
         vars=shape(2, 0)),
    dict(id="e2e-curve-small", figure="curve500", x=500, label="500-transaction batches",
         mode="curve", rates=[10000, 25000, 50000, 100000, 150000, 200000, 250000, 300000],
         vars=shape(2, 0)),

    # Step 3: one setup, transaction size only. The paper's Figure 7b sizes; seeds are its own ratios
    # against its 300-byte point, so one FX_SEED scales the sweep. Size is set through the read-write
    # value, and the rendered value size is read back before each point is measured, because a size that
    # silently failed to apply is exactly how a committer point came to be mislabelled earlier.
    dict(id="e2e-size300", figure="size", x=300, label="300 B transactions", seed=BASE_SEED,
         vars=dict(shape(2, 0), loadgen_read_write_tx_val_size=49)),
    dict(id="e2e-size512", figure="size", x=512, label="512 B transactions",
         seed=int(BASE_SEED * 0.59), vars=dict(shape(2, 0), loadgen_read_write_tx_val_size=155)),
    dict(id="e2e-size1024", figure="size", x=1024, label="1 KB transactions",
         seed=int(BASE_SEED * 0.30), vars=dict(shape(2, 0), loadgen_read_write_tx_val_size=411)),
    dict(id="e2e-size2048", figure="size", x=2048, label="2 KB transactions",
         seed=int(BASE_SEED * 0.45), vars=dict(shape(2, 0), loadgen_read_write_tx_val_size=923)),
    dict(id="e2e-size3072", figure="size", x=3072, label="3 KB transactions",
         seed=int(BASE_SEED * 0.39), vars=dict(shape(2, 0), loadgen_read_write_tx_val_size=1435)),
    dict(id="e2e-size4096", figure="size", x=4096, label="4 KB transactions",
         seed=int(BASE_SEED * 0.34), vars=dict(shape(2, 0), loadgen_read_write_tx_val_size=1947)),
]

if os.environ.get("FX_MATRIX") == "e2e":
    EXPERIMENTS = E2E_EXPERIMENTS

# The e2e stages need different inventories (4-shard vs 8-shard) and different shared configs
# (block size), so they cannot all run under one deployment. FX_ONLY picks the rows that match the
# deployment that is actually up: a comma-separated list of ids or id prefixes.
#
# A pattern ending in "$" is ANCHORED and matches that id exactly. Without it every pattern is a
# prefix, and the share ids are prefixes of each other -- "9c-ds1" also selects "9c-ds10", which is a
# different conflict share and a batch nobody asked for. A selector that silently widens is the same
# defect as an unanchored -bench pattern, and it costs a bring-up to notice.
ONLY = [p for p in os.environ.get("FX_ONLY", "").split(",") if p]
if ONLY:
    def _selected(eid):
        return any(eid == p[:-1] if p.endswith("$") else eid.startswith(p) for p in ONLY)
    EXPERIMENTS = [e for e in EXPERIMENTS if _selected(e["id"])]
    if not EXPERIMENTS:
        sys.exit(f"FX_ONLY={os.environ['FX_ONLY']} matched no experiment")


def query(expr):
    cmd = ["curl", "-sk", "--max-time", "20", "-G",
           "--data-urlencode", f"query={expr}", f"{PROM}/api/v1/query"]
    try:
        payload = json.loads(subprocess.run(
            cmd, capture_output=True, text=True, timeout=25).stdout)
    except (subprocess.TimeoutExpired, json.JSONDecodeError):
        return None, None
    result = payload.get("data", {}).get("result") or []
    if not result:
        return None, None
    value = float(result[0]["value"][1])
    return (None if value != value else value), result[0].get("metric", {}).get("instance")


def sample():
    snap = {}
    for name, expr in QUERIES.items():
        value, label = query(expr)
        snap[name] = value
        if name == "cpu_busiest":
            snap["cpu_busiest_host"] = label
    return snap


def make(target, extra_vars=False, timeout=3600):
    """Run one make target against the committer inventory, optionally with the shape file."""
    playbook = ".venv/bin/ansible-playbook"
    if extra_vars:
        playbook = f"{playbook} --extra-vars @{VARS_FILE}"
    cmd = (f"source /data1/cluster/bin/fx-env.sh && ANSIBLE_INVENTORY={INVENTORY} "
           f"make {target} ANSIBLE_PLAYBOOK='{playbook}'")
    r = subprocess.run(cmd, shell=True, executable="/bin/bash", cwd=PROJECT,
                       capture_output=True, text=True, timeout=timeout)
    if r.returncode != 0:
        log(f"!! make {target} failed rc={r.returncode}")
        log(r.stdout[-2000:])
        log(r.stderr[-800:])
    return r.returncode == 0


def bringup():
    """Stop, wipe, setup, gate on the crypto, start, init, gate on a committed rate.

    The one sequence that works on the real-orderer arm, kept in fx-bringup.sh so the standalone
    bring-up and a per-point redeploy cannot drift apart.
    """
    cmd = f"cd /data1/logs && EXTRA=1 INV={INVENTORY} ./fx-bringup.sh"
    r = subprocess.run(cmd, shell=True, executable="/bin/bash",
                       capture_output=True, text=True, timeout=7200)
    if r.returncode != 0:
        log(f"!! bring-up failed rc={r.returncode}")
        log(r.stdout[-2500:])
    return r.returncode == 0


def set_rate(rate):
    return make(f"limit-rate LIMIT={rate}", timeout=900)


LOG = None


def log(line):
    stamp = time.strftime("%H:%M:%S")
    print(f"{stamp} {line}", flush=True)
    if LOG:
        LOG.write(f"{stamp} {line}\n")
        LOG.flush()


def record(row):
    with open(OUT, "a") as f:
        f.write(json.dumps(row) + "\n")


# The load generator's rendered config, on the machine that reads it. cluster.yaml names the
# generator host orderer-loadgen because it also serves as the mock ordering service.
LOADGEN_CONFIG = "/data1/fabric-x/orderer-loadgen/config/config-loadgen.yaml"


def rendered_shape():
    """The shape the load generator will actually run, read back off its own config file."""
    r = subprocess.run(
        ["ssh", "-o", "StrictHostKeyChecking=no", "loadgen",
         f"grep -E '^ +(read-write-count|write-count|invalid-signatures|key-backref-rate|"
         f"read-write-value-size):' "
         f"{LOADGEN_CONFIG}"],
        capture_output=True, text=True, timeout=60)
    shape = {}
    for line in r.stdout.splitlines():
        key, _, value = line.strip().partition(":")
        shape[key] = value.strip()
    return shape


def deploy(exp):
    """Re-render the load generator for this shape and start it on empty runtime state.

    Teardown comes first and configs second, in that order. Teardown removes the whole remote
    deploy directory, so shipping the configs before it deletes exactly what was just shipped and
    the generator comes back up on the previous shape -- which is how the first attempt at this
    measured 1/1 with a 2/2 config.

    On the real-orderer arm `configs` is not enough to put the cluster back. Teardown takes the
    whole remote deploy directory, which on that arm holds the generated crypto and each node's
    genesis block; `configs` only re-renders configuration, so the orderer comes back up with no
    identity. `setup` (binaries, crypto, genesis, configs) is what rebuilds it -- and regenerating
    crypto needs the Fabric CA's stale enrollment cleared first, because teardown drops the CA's
    database while leaving the admin's enrolled MSP on disk, so the next enrollment presents a
    certificate the fresh registry has never issued and crypto generation stops at
    "Authentication failure".
    """
    with open(VARS_FILE, "w") as f:
        json.dump(exp["vars"], f, indent=2)   # JSON is valid YAML, and needs no quoting rules
    if SKIP_DEPLOY:
        shape = rendered_shape()
        if not shape_matches(exp, shape):
            log(f"[{exp['id']}] rendered shape {shape} does not match this experiment and "
                f"FX_SKIP_DEPLOY is set; skipping rather than redeploying")
            return False
        log(f"[{exp['id']}] measuring the running deployment, rendered shape {shape}")
        return True
    if DEPLOY_PLAN == "none":
        # Verify the running deployment is the shape this point wants, and measure it as it stands. The
        # read-back is the whole safety net here: nothing was re-rendered, so a mismatch would mean this
        # point is not the experiment it claims to be.
        shape = rendered_shape()
        want_rw = str(exp["vars"]["loadgen_read_write_tx_keys"])
        if shape.get("read-write-count") != want_rw:
            log(f"[{exp['id']}] running deployment is {shape}, not {want_rw} read-writes; skipping")
            return False
        log(f"[{exp['id']}] measuring the running deployment as it stands, shape {shape}")
        return True
    log(f"[{exp['id']}] teardown + {DEPLOY_PLAN} + start: {exp['vars']}")
    # Everything that holds runtime state, and nothing that does not. An unfiltered teardown also takes
    # the Fabric CA and the monitoring stack, which costs twice over: the CA's database goes while its
    # admin MSP stays on disk, so the next `setup` presents a certificate the fresh registry never
    # issued and stops at "Authentication failure" -- that killed a six-point size sweep on all six
    # points; and Prometheus, Grafana, Loki and Alloy are destroyed and rebuilt around every single
    # measurement, which discards the metric history a run is meant to leave behind and makes the
    # dashboard flap for anyone watching.
    # Best effort, as it is in fx-bringup.sh. Teardown removes state that the `configs` and `start` below
    # replace anyway, and it fails for reasons that do not matter: run seconds after a bring-up's gate
    # passes it caught verifiers still initialising, reported failed=1 on every host, and aborted a whole
    # ladder -- after which the batch queued behind it was refused by the one-driver guard, because the
    # teardown it had given up on was still running. The same teardown succeeded a minute later. A failed
    # `configs`, or a rendered shape that does not match, are still fatal below.
    if not make(f"teardown TARGET_HOSTS={TEARDOWN_HOSTS}", extra_vars=True):
        log(f"[{exp['id']}] teardown reported a failure; continuing, since configs and start replace "
            f"what it removes")
    if DEPLOY_PLAN == "setup":
        # A per-point redeploy on this arm is a full bring-up, and it has to be. `teardown` drops the
        # Fabric CA's database while leaving the admin's enrolled MSP on disk, so the next `setup`
        # presents a certificate the fresh registry never issued and crypto generation stops at
        # "Authentication failure" -- which is exactly how a six-point size sweep failed on all six.
        # Wiping only the CA is worse: it re-keys the CA while every host keeps an identity the new key
        # did not sign, which killed six earlier bring-ups with an assembler rejecting a genesis block
        # whose org MSP could not verify itself. Nothing anywhere may predate the new key.
        #
        # fx-bringup.sh is that sequence, already gated on the crypto and on a committed rate, so this
        # calls it rather than growing a second copy of it here. EXTRA=1 makes it render this
        # experiment's shape file instead of the inventory's defaults.
        if not bringup():
            return False
        return True
    if not make("configs", extra_vars=True):
        return False
    # Verify the artifact rather than the exit code: a shape that silently failed to apply would
    # otherwise be reported as a measurement of the shape that was asked for.
    shape = rendered_shape()
    if not shape_matches(exp, shape):
        log(f"[{exp['id']}] rendered shape {shape} is not what was requested; skipping")
        return False
    log(f"[{exp['id']}] rendered shape {shape}")
    if not make("start", extra_vars=True):
        return False
    return wait_healthy(exp)


def shape_matches(exp, shape):
    """Whether the generator's rendered config is the workload this experiment asked for.

    Verify the artifact rather than the exit code: a shape that silently failed to apply would otherwise
    be reported as a measurement of the shape that was requested, which is how an earlier point in this
    evaluation came to be labelled 1/1 while running 2/2.
    """
    want_rw = str(exp["vars"]["loadgen_read_write_tx_keys"])
    # The template omits write-count entirely when blind-write generation is off, so a shape with no
    # outputs is verified by that absence rather than by a zero.
    outputs = exp["vars"]["loadgen_write_only_tx_keys"]
    want_w = str(outputs) if outputs else None
    if shape.get("read-write-count") != want_rw or shape.get("write-count") != want_w:
        return False
    # The size sweep is verified the same way. The collection's variable is
    # `loadgen_read_write_tx_val_size`, which is not the config key it renders, so a rename upstream would
    # break silently rather than loudly.
    want_value = exp["vars"].get("loadgen_read_write_tx_val_size")
    if want_value is not None and shape.get("read-write-value-size") != str(want_value):
        return False
    return True


def wait_healthy(exp):
    """Wait for the pipeline to commit, not merely for Ansible to return."""
    # A component that is up is not a component that is committing, so wait for the pipeline to
    # actually move transactions rather than for Ansible to return.
    for attempt in range(40):
        time.sleep(15)
        s = sample()
        if (s.get("down") or 0) == 0 and (s.get("committed") or 0) > 0:
            log(f"[{exp['id']}] healthy after {(attempt + 1) * 15}s"
                f" ({s['committed']:,.0f} tx/s at the idle rate)")
            return True
    log(f"[{exp['id']}] never became healthy")
    return False


def measure(exp, rate, settle, window, kind):
    """Hold one rate and report what the cluster did over the window."""
    free = query(QUERIES["disk_free_gb"])[0]
    if free is not None and free < DISK_FLOOR_GB:
        log(f"[{exp['id']}] disk floor reached ({free:,.0f} GB free); skipping {rate:,}")
        return None
    if not set_rate(rate):
        return None
    time.sleep(settle)
    s0 = sample()
    time.sleep(window)
    s = sample()

    committed, offered, p99 = s.get("committed"), s.get("offered"), s.get("lat_p99")
    # A transaction the committer rejects is finished, not outstanding, and the paper counts it:
    # its Figure 9b reports throughput RISING as the invalid share rises, which is only possible if
    # rejected transactions count. So the rate to compare against the offered rate, and the rate to
    # subtract when asking whether a queue is growing, is committed plus aborted.
    finished = None if committed is None else committed + (s.get("aborted") or 0)

    def outstanding(snap):
        if snap.get("sent_total") is None or snap.get("committed_total") is None:
            return None
        return snap["sent_total"] - snap["committed_total"] - (snap.get("aborted_total") or 0)

    growth = None
    if outstanding(s) is not None and outstanding(s0) is not None:
        growth = (outstanding(s) - outstanding(s0)) / window

    # The rate averaged over the WINDOW, from the counter at each end of it, against `offered` which is a
    # 60 s rate sampled at the window's close. The pair catches a rung that ramped, stalled, or straddled an
    # interruption -- a rung can close at its nominal rate having spent much of the window somewhere else,
    # and `met` would see nothing wrong. Measured on ds30: 24,728 against 25,000 nominal and 80,114 against
    # 80,000, so a real rung tracks to ~1% and a bound of 20% cannot reject one.
    #
    # Bounded to the window deliberately. Taken between arbitrary samples the elapsed term would contain
    # any redeploy that happened in between, so it would flag every post-redeploy rung -- including the
    # bridge rung, whose whole purpose is to validate one. That form would reject exactly what it exists
    # to check. s0 and s are the window's own endpoints, so a redeploy before it cannot dilute the average.
    sent_window = None
    if s.get("sent_total") is not None and s0.get("sent_total") is not None:
        sent_window = (s["sent_total"] - s0["sent_total"]) / window

    # All four conditions, so that a reported point is one the cluster could hold: the rate
    # arrived, it was committed, the latency met the paper's bound, and nothing was accumulating
    # behind it.
    # Finishing MORE than was offered is arithmetically impossible in steady state, so it is a definitive
    # statement that the window measured a backlog draining rather than the rate. It is a cleaner flag than
    # growth (a queue pinned at its ceiling has zero derivative) or mean latency (which needs a threshold),
    # and it was the defect behind every hold in this dataset: the driver holds at the last PASSING probe,
    # which after a 17-rung climb is the top of the climb -- a rate that clears 90 s and not 300 s -- so
    # hold 1 fails and the step-downs below it measure hold 1's backlog. Rows show it plainly:
    # offered 95,157 / finished 113,273, offered 88,108 / finished 135,455, and a "met" hold at growth
    # -12,433/s. As a condition of `met` rather than a flag on the row, no such window can be reported as a
    # rate again -- which is also the root cause of the 300 B size point landing at 408,000.
    met = (finished is not None and finished >= rate * (1 - TOLERANCE)
           # The allowance is proportional PLUS an absolute floor, because 2% of a low rung is nothing:
           # 2% of 2,500 tps is 50, and rate-limiter jitter over a 300 s window is comfortably that
           # (rung 1 here came in 91 tps over its 15,000). A draining queue overshoots by thousands --
           # the rows that motivated this test ran 19% and 54% over -- so 200 tps of slack cannot hide one.
           and finished <= (offered or 0) + max(200.0, (offered or 0) * TOLERANCE)
           and (offered or 0) >= rate * (1 - TOLERANCE)
           and p99 is not None and p99 <= SLO_P99
           # Two-sided, because a window that DRAINS is as unrepresentative as one that accumulates: the
           # latency in it belongs to transactions offered earlier, at a different rate. The one-sided
           # form admitted ds30's 50,000 rung as MET at grow -6,800/s -- 13.6% of the rate -- while the
           # rung below it at 25,000 had failed, so the panel's 30% point would have been a drain and
           # would have contradicted the miss beneath it. The arrival check does not catch this case: both
           # the offered and finished rates read 50,000 and only the growth term reveals it. Every clean
           # rung measured so far reports growth of exactly 0, so the two-sided bound costs nothing.
           and (growth is None or abs(growth) <= rate * TOLERANCE)
           and (sent_window is None or abs(sent_window - rate) <= rate * 0.2)
           and (s.get("append_util") or 0) < 0.95)

    # `histogram_quantile` returns the top finite bucket boundary once the quantile falls in the +Inf
    # bucket, so an overloaded row reports a LIMIT that reads exactly like a measurement -- 60,000 ms
    # appears in 114 rows across the two results files. The independent proof is a mean above the 99th
    # percentile, which is impossible: ladder5m's first rung reported p99 60,000 ms with a mean of 69,334.
    #
    # What is NOT censoring, checked against the histogram's own `le` set: 14,950 / 29,900 / 44,850 are
    # INTERPOLATIONS inside the 10-15 s, 20-30 s and 30-45 s buckets, sitting near their tops. Coarse,
    # because the buckets are 5-15 s wide up there, but real. That distinction decides a result: the six
    # 8-tablet holds all read exactly 29,900 ms, so they genuinely ran at ~30 s rather than being clamped,
    # and 8 tablets is a measured failure at those rates rather than an unknown.
    #
    # Recorded per row because a censored p99 still fails the SLO correctly, so nothing was mis-accepted,
    # but nothing can be QUOTED from such a row either, and five of today's six defects were values that
    # read as measurements while being limits. `lat_mean` is sum/count and stays valid, so mean is the
    # statistic for overload and p99 only near the bound.
    mean = s.get("lat_mean")
    censored = (p99 is not None and p99 >= P99_CEILING) or (
        p99 is not None and mean is not None and mean > p99)
    row = {"experiment": exp["id"], "figure": exp["figure"], "x": exp["x"],
           "label": exp["label"], "kind": kind, "limit": rate, "met": met,
           "finished": finished, "window": window, "at": time.time(),
           "inflight_growth": growth, "lat_p99_censored": censored,
           "sent_rate_window": sent_window, "vars": exp["vars"],
           **{k: s.get(k) for k in QUERIES if k != "cpu_busiest"},
           "cpu_busiest_host": s.get("cpu_busiest_host")}
    record(row)
    log(f"[{exp['id']}] {kind} limit={rate:,} finished={fmt(finished)} committed={fmt(committed)} "
        f"abort={fmt(s.get('aborted'))} p99={fmt(ms(p99), 0)}ms{'(CENSORED)' if censored else ''} "
        f"mean={fmt(ms(mean), 0)}ms "
        f"grow={fmt(growth)}/s app={pct(s.get('append_util'))} cpu={pct(s.get('cpu_max'))} "
        f"({s.get('cpu_busiest_host') or '-'}) -> {'MET' if met else 'MISS'}")
    return row


def fmt(v, digits=0):
    return "-" if v is None else f"{v:,.{digits}f}"


def ms(v):
    return None if v is None else v * 1000


def pct(v):
    return "-" if v is None else f"{v * 100:.0f}%"


DRAIN_RATE = int(os.environ.get("FX_DRAIN_RATE", "20000"))


def drain(exp, rounds=4):
    """Park the rate low until the in-flight count stops falling.

    A probe above the knee leaves a queue of millions of transactions, and the next measurement at
    a rate near capacity has no spare throughput to clear it: it reports the queue's drain time as
    latency. Observed at 3 read-writes -- a probe met 420,000 tps at 374 ms, the probe above it
    pinned the sidecar's waiting set at its 500,000 limit, and the confirmation hold at the same
    420,000 then reported 9,975 ms and was rejected.

    The drain rate has to be far below the workload's capacity, not merely below the rate being
    searched, and the default of 20,000 is not that for a conflict workload whose capacity IS about
    20,235. Parked there the queue does not fall at all: a 5% double-spend ladder logged 1,830,000 in
    flight falling to 1,430,000 and then RISING to 3,340,000 while nominally draining, and every rung
    after the first reported the previous rung's backlog as its own latency -- 148,836 ms at 23,000 tps
    is a 3.4M queue, not a measurement. Set FX_DRAIN_RATE well under capacity for such a workload.
    """
    # A tenth of what the pipeline is currently retiring, floored at 1,000, rather than a fixed rate.
    # A fixed 20,000 cannot drain a workload whose capacity IS 20,235: the backlog never shrinks and the
    # give-up test below then fires on the first comparison, abandoning the drain with millions queued.
    # Measuring the served rate first makes the parked rate correct for any workload.
    served = (sample().get("committed") or 0)
    rate = max(1000, int(0.1 * served)) if served else DRAIN_RATE
    if rate != DRAIN_RATE:
        log(f"[{exp['id']}] draining at {rate:,} tx/s, a tenth of the {served:,.0f} being retired")
    if not set_rate(rate):
        return
    previous, stalled = None, False
    for _ in range(rounds):
        time.sleep(60)
        s = sample()
        if s.get("sent_total") is None or s.get("committed_total") is None:
            return
        inflight = s["sent_total"] - s["committed_total"] - (s.get("aborted_total") or 0)
        log(f"[{exp['id']}] draining: {inflight:,.0f} in flight, "
            f"sidecar waiting {(s.get('sc_waiting') or 0):,.0f}")
        # Stop when the queue is small, or when two consecutive samples fail to improve on it. One
        # non-improving sample is not enough: it fires on a single noisy reading, and when the drain rate
        # is near capacity it fires immediately and permanently, which is how a contaminated ladder
        # passes for a measured one.
        if inflight < 4 * rate:
            return
        if previous is not None and inflight >= previous:
            if stalled:
                log(f"[{exp['id']}] draining is not reducing the queue at {rate:,} tx/s; "
                    f"capacity is likely at or below that rate")
                return
            stalled = True
        else:
            stalled = False
        previous = inflight


def search(exp, settle, window):
    """Find the highest rate that meets every condition, starting from the shape's seed.

    The first probe is drained into like every other one. Without that it can measure a backlog the
    bring-up left -- the health gate runs the generator before any rate is set -- and a probe measuring
    a drain reports MORE than it was offered with a tail to match, which the search reads as the seed
    being too high and steps down from. That is what put the 300 B size point at 408,000 tps: its first
    probe delivered 492,182 against 480,000 offered at a 1,354 ms tail, while the same workload on the
    shard ladder held 499,091 the same day.
    """
    rate, best = exp["seed"], None
    drain(exp, rounds=2)
    first = measure(exp, rate, settle, window, "probe")
    if first is None:
        return None
    if first["met"]:
        best = first
        # The climb is bounded, so a seed far below the knee under-reports it: the search ends with
        # every step met and the knee never bracketed. UP_STEPS raises that ceiling for a point whose
        # seed turned out to be pessimistic, and the log line to look for is a search whose last probe
        # met its rate.
        for _ in range(UP_STEPS):
            rate = int(rate * 1.08)
            row = measure(exp, rate, settle, window, "probe")
            if row is None or not row["met"]:
                break
            best = row
    else:
        # A descent step after a saturated step measures the PREVIOUS step's backlog, not its own rate,
        # and no drain heuristic has survived a day of use: parked at a fixed rate it no-opped when the
        # rate matched capacity, parked at a tenth of retirement it still leaves a tail that needs ~320 s
        # to clear against a 90 s window. The 18,423 probe at 96 tablets is the demonstration -- it
        # sustained its offered rate (grow -556/s) and still missed at a 7.3 s mean, because that mean was
        # the previous rung draining. Nor can `grow` gate it: a queue pinned at its ceiling has zero
        # derivative, so the tab88 rows at 8.6x capacity read -1,111/s and would pass any growth filter.
        # A redeploy is the protocol this project already uses for holds and it has no heuristic in it.
        for _ in range(6):
            if not deploy(exp):
                break
            rate = int(rate * 0.85)
            row = measure(exp, rate, settle, window, "probe")
            if row is None:
                break
            if row["met"]:
                best = row
                break
    return best


def main():
    global LOG
    only = sys.argv[1] if len(sys.argv) > 1 else None
    # The Prometheus queries average over 60 s, so a settle shorter than that averages part of
    # the previous rate into this one's first sample.
    settle = int(os.environ.get("FX_SETTLE", "75"))
    window = int(os.environ.get("FX_WINDOW", "90"))
    hold = int(os.environ.get("FX_HOLD", "300"))
    deadline = time.time() + float(os.environ.get("FX_DEADLINE_HOURS", "11")) * 3600
    LOG = open("/data1/logs/figures.log", "a")
    done = set()
    if os.path.exists(OUT):
        with open(OUT) as f:
            for line in f:
                row = json.loads(line)
                if row.get("kind") in ("hold", "curve") and row.get("met"):
                    done.add(row["experiment"])

    log(f"=== figures run: slo_p99={SLO_P99}s settle={settle}s window={window}s hold={hold}s "
        f"deadline={os.environ.get('FX_DEADLINE_HOURS', '11')}h inventory={INVENTORY}")

    for exp in EXPERIMENTS:
        if only and only not in exp["id"]:
            continue
        if exp["id"] in REDO:
            log(f"=== {exp['id']}: re-running despite a confirmed hold (FX_REDO)")
        elif exp["id"] in done:
            log(f"=== {exp['id']}: already has a confirmed hold; skipping")
            continue
        if time.time() > deadline:
            log("deadline reached; stopping before " + exp["id"])
            break
        log(f"=== {exp['id']}: {exp['label']} ({exp['figure']})")
        try:
            if not deploy(exp):
                continue
            if exp.get("mode") == "curve":
                passed = None
                for rate in exp["rates"]:
                    if time.time() > deadline:
                        break
                    row = measure(exp, rate, settle, hold, "curve")
                    if row is not None and row["met"]:
                        passed = rate
                        continue
                    # The ladder climbs past the knee, and everything after that point would otherwise
                    # measure the queue the previous rate built. Redeployed rather than drained for the
                    # reason given in search(): every drain heuristic tried has left a tail longer than
                    # the next window, and a sustained-but-late rung is indistinguishable from a slow one.
                    if not deploy(exp):
                        break
                    # But a redeploy trades one confound for another: rungs after it run on a different
                    # table from rungs before it, so a ladder that redeploys mid-way cannot tell a RATE
                    # effect from a DEPLOYMENT effect. That is not hypothetical -- ds30 read pass, fail,
                    # pass across 10,000 / 25,000 / 80,000, and the two passes sat on either side of a
                    # redeploy, so the non-monotonicity was never demonstrated within one deployment and
                    # three sessions reversed on it five times in ninety minutes.
                    #
                    # So repeat the last rate that PASSED on the new deployment. If it reproduces, the two
                    # halves of the ladder are comparable and a later rung's result is its own; if it does
                    # not, the redeploy moved something and every rung after it is suspect. One rung per
                    # miss, and nothing at all on a ladder that never misses.
                    if passed is not None:
                        log(f"[{exp['id']}] bridging: repeating {passed:,} on the new deployment, so a "
                            f"later rung is separable from the redeploy")
                        measure(exp, passed, settle, hold, "bridge")
                continue
            best = search(exp, settle, window)
            if best is None:
                log(f"[{exp['id']}] no rate met the conditions")
                continue
            # The search ends above the knee by construction, so the pipeline it leaves behind is
            # not one to measure in. The reported number comes from a deployment of its own.
            #
            # A hold that fails steps down a rung and tries again, rather than leaving the point with
            # no confirmed number. That is not a formality: at the rate a 90 s probe accepts, a 300 s
            # hold has three times as long to accumulate a queue, and on the baseline shape's marginal
            # rung the probes passed three of four attempts while the holds passed one of three. The
            # rate the search picks is therefore optimistic about a third of the time, and without
            # this the point is simply lost.
            rate = best["limit"]
            for attempt in range(3):
                if not deploy(exp):
                    break
                # A pre-check against the floor is not enough for a high-byte-rate workload: the
                # floor was clear when the 4 KiB hold started and the hold itself wrote through the
                # remaining 135 GB, filled every assembler volume to 4.9 MB free, and killed the arma
                # process on all four. Require room for what this hold will actually write.
                #
                # Each assembler writes the COMPLETE block stream, so the volume that matters sees the
                # whole payload, not a shard of it. +20% for block framing and metadata.
                envelope = exp.get("x") if exp.get("figure") == "size" else 262
                need_gb = rate * envelope * hold * 1.2 / 1e9
                free = query(QUERIES["disk_free_gb"])[0]
                if free is not None and free < need_gb + DISK_FLOOR_GB:
                    log(f"[{exp['id']}] a {hold}s hold at {rate:,} would write "
                        f"{need_gb:,.0f} GB and only {free:,.0f} GB is free; not attempting it. "
                        f"The bracketed knee stands as a probe.")
                    break
                if SKIP_DEPLOY:
                    # deploy() only verified the rendered shape -- it did not redeploy -- so the
                    # queue the search left above the knee is still in the pipeline, and this hold
                    # would report its drain time as latency. Observed exactly that: a hold at
                    # 479,999 delivered 502,545 (more than offered, because it was draining) with
                    # p99 15 s and the in-flight count falling 30,767/s, then stepped down against a
                    # baseline that was never the rate's own. Drain first, and give it more rounds
                    # than the in-search drain: past the knee the backlog is millions deep.
                    drain(exp, rounds=8)
                log(f"[{exp['id']}] confirming {rate:,} for {hold}s"
                    f"{' on a fresh deployment' if not SKIP_DEPLOY else ', drained'}"
                    f"{'' if attempt == 0 else f' (attempt {attempt + 1})'}")
                row = measure(exp, rate, settle, hold, "hold")
                if row is None:
                    log(f"[{exp['id']}] the hold could not be measured; not stepping down, since a "
                        f"lower rate faces the same limit. The bracketed knee stands as a probe.")
                    break
                if row["met"]:
                    break
                rate = int(rate / 1.08)
                log(f"[{exp['id']}] the hold did not hold; stepping down to {rate:,}")
        except Exception as e:                                  # unattended: never stop early
            log(f"[{exp['id']}] failed: {type(e).__name__}: {e}")

    log("=== figures run done")


if __name__ == "__main__":
    main()
