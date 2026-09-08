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
OUT = os.environ.get("FX_OUT", "/data1/logs/figures.jsonl")
VARS_FILE = "/data1/logs/exp-vars.yaml"

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
        # The window decides what a "double spend" means here, and it matters more than the conflict
        # rate. A reference is drawn from the newest `key_lookback_window` keys, and at ~500,000
        # transactions a second the 1024 newest were created about two milliseconds ago -- so with a
        # 1024-key window every reference targets a key still deep inside a ~500 ms pipeline, every
        # conflicting transaction blocks behind one in flight, and the convoy swallows the workload:
        # measured at 5% back-references, throughput fell from 518,000 to 71,455 tps with a 60 s tail,
        # a 7.3x collapse that says nothing about double spends and everything about contention
        # concentration.
        #
        # 10,000,000 keys is about twenty seconds of production at these rates, so a reference points
        # at a key that is almost always already committed. That is the paper's double spend: two
        # transactions spend the same input, the first commits, and the second is rejected by read
        # validation for reading a key whose committed version contradicts its nil expectation.
        v["loadgen_tx_reference_gap"] = 0
        v["loadgen_key_lookback_window"] = 10_000_000
    return v


BASE_SEED = int(os.environ.get("FX_SEED", "480000"))
# Points to measure again even though the output already holds a confirmed hold for them. The first
# two points of the size sweep were measured before the driver held its confirmation on a fresh
# deployment, so they ran against a table holding several times as many rows as the later points --
# and commit latency rises with the table, at about 0.18 ms per million transactions committed.
REDO = set(filter(None, os.environ.get("FX_REDO", "").split(",")))
UP_STEPS = int(os.environ.get("FX_UP_STEPS", "4"))

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
    dict(id="9c-ds0", figure="9c", x=0, label="0% double spend", seed=BASE_SEED,
         vars=shape(2, 0, backref=0.0)),
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

    # The mechanism, tested by removing it. A conflict workload holds ~6 seconds of latency per
    # transaction at 2,000 tps on a fresh deployment with the cluster at 7% CPU -- so it is not a queue
    # and not capacity. The validator-committer retries a database batch with an exponential backoff
    # starting at 500 ms (`initial-interval`, multiplier 1.5, +/-50% jitter) and can retry twice, once
    # for inserting keys that already exist. With about 377 transactions per batch, a 5% per-transaction
    # conflict rate puts a conflict in essentially EVERY batch (1 - 0.95^377), so every batch pays
    # 500-1,250 ms of backoff. That produces multi-second latency at any rate, a collapse to ~19,000
    # tps, and nothing saturated anywhere -- all four of which are observed.
    #
    # 5 ms initial interval keeps the retry behaviour and removes the wait. If throughput recovers, the
    # backoff is the mechanism and the fix is a configuration one; if it does not, the retry is not what
    # costs the six seconds.
    dict(id="9c-ds5-fastretry", figure="retry", x=5, label="5% double spend, 5ms retry backoff",
         mode="curve", rates=[2000, 20000, 100000],
         vars=dict(shape(2, 0, backref=0.05),
                   committer_database_retry_initial_interval="5ms")),

    # Cause or effect. During the conflict collapse the dependency graph sits pinned at its 500,000
    # admission limit (`committer_coordinator_dep_graph_wait_tx_limit`) while validation runs at 2.2 ms
    # and commit at 21 ms -- both healthy. But throughput x latency = 20,000 x 25 s = 500,000, which is
    # exactly the limit, so a full graph is what ANY admission-controlled pipeline looks like when
    # something downstream is slow. Raising the limit to the role default separates the two: if
    # throughput rises, the limit was throttling a pipeline that could have gone faster; if throughput
    # holds at 20,000 and the graph simply grows past 500,000 with latency rising to match, the limit
    # is innocent and the slowness is elsewhere.
    dict(id="9c-ds5-bigraph", figure="graphlimit", x=5, label="5% double spend, 20M graph limit",
         seed=30000,
         vars=dict(shape(2, 0, backref=0.05),
                   committer_coordinator_dep_graph_wait_tx_limit=20000000)),

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
    dict(id="9a-utxo1", figure="9a-utxo", x=1, label="1 in / 1 out", seed=300000,
         vars=shape(1, 1)),
    dict(id="9a-utxo4", figure="9a-utxo", x=4, label="4 in / 4 out", seed=200000,
         vars=shape(4, 4)),

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
         f"grep -E '^ +(read-write-count|write-count|invalid-signatures|key-backref-rate):' "
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
    """
    with open(VARS_FILE, "w") as f:
        json.dump(exp["vars"], f, indent=2)   # JSON is valid YAML, and needs no quoting rules
    log(f"[{exp['id']}] teardown + configs + start: {exp['vars']}")
    if not make("teardown", extra_vars=True):
        return False
    if not make("configs", extra_vars=True):
        return False
    # Verify the artifact rather than the exit code: a shape that silently failed to apply would
    # otherwise be reported as a measurement of the shape that was asked for.
    shape = rendered_shape()
    want_rw = str(exp["vars"]["loadgen_read_write_tx_keys"])
    # The template omits write-count entirely when blind-write generation is off, so a shape with
    # no outputs is verified by that absence rather than by a zero.
    outputs = exp["vars"]["loadgen_write_only_tx_keys"]
    want_w = str(outputs) if outputs else None
    if shape.get("read-write-count") != want_rw or shape.get("write-count") != want_w:
        log(f"[{exp['id']}] rendered shape {shape} is not the requested "
            f"{want_rw} in / {want_w} out; skipping")
        return False
    log(f"[{exp['id']}] rendered shape {shape}")
    if not make("start", extra_vars=True):
        return False
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

    # All four conditions, so that a reported point is one the cluster could hold: the rate
    # arrived, it was committed, the latency met the paper's bound, and nothing was accumulating
    # behind it.
    met = (finished is not None and finished >= rate * (1 - TOLERANCE)
           and (offered or 0) >= rate * (1 - TOLERANCE)
           and p99 is not None and p99 <= SLO_P99
           and (growth is None or growth <= rate * TOLERANCE)
           and (s.get("append_util") or 0) < 0.95)

    row = {"experiment": exp["id"], "figure": exp["figure"], "x": exp["x"],
           "label": exp["label"], "kind": kind, "limit": rate, "met": met,
           "finished": finished, "window": window, "at": time.time(),
           "inflight_growth": growth, "vars": exp["vars"],
           **{k: s.get(k) for k in QUERIES if k != "cpu_busiest"},
           "cpu_busiest_host": s.get("cpu_busiest_host")}
    record(row)
    log(f"[{exp['id']}] {kind} limit={rate:,} finished={fmt(finished)} committed={fmt(committed)} "
        f"abort={fmt(s.get('aborted'))} p99={fmt(ms(p99), 0)}ms mean={fmt(ms(s.get('lat_mean')), 0)}ms "
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
    """
    if not set_rate(DRAIN_RATE):
        return
    previous = None
    for _ in range(rounds):
        time.sleep(60)
        s = sample()
        if s.get("sent_total") is None or s.get("committed_total") is None:
            return
        inflight = s["sent_total"] - s["committed_total"] - (s.get("aborted_total") or 0)
        log(f"[{exp['id']}] draining: {inflight:,.0f} in flight, "
            f"sidecar waiting {(s.get('sc_waiting') or 0):,.0f}")
        # Stop when the queue is small, or when it has stopped shrinking (nothing more to gain).
        if inflight < 4 * DRAIN_RATE or (previous is not None and inflight >= previous):
            return
        previous = inflight


def search(exp, settle, window):
    """Find the highest rate that meets every condition, starting from the shape's seed."""
    rate, best = exp["seed"], None
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
        drain(exp)
        for _ in range(6):
            rate = int(rate * 0.85)
            row = measure(exp, rate, settle, window, "probe")
            if row is None:
                break
            if row["met"]:
                best = row
                break
            drain(exp)
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
                for rate in exp["rates"]:
                    if time.time() > deadline:
                        break
                    row = measure(exp, rate, settle, hold, "curve")
                    # The ladder climbs past the knee, and everything after that point would
                    # otherwise measure the queue the previous rate built.
                    if row is not None and not row["met"]:
                        drain(exp)
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
                log(f"[{exp['id']}] confirming {rate:,} for {hold}s on a fresh deployment"
                    f"{'' if attempt == 0 else f' (attempt {attempt + 1})'}")
                row = measure(exp, rate, settle, hold, "hold")
                if row is not None and row["met"]:
                    break
                rate = int(rate / 1.08)
                log(f"[{exp['id']}] the hold did not hold; stepping down to {rate:,}")
        except Exception as e:                                  # unattended: never stop early
            log(f"[{exp['id']}] failed: {type(e).__name__}: {e}")

    log("=== figures run done")


if __name__ == "__main__":
    main()
