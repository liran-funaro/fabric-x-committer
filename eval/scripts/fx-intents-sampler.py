#!/usr/bin/env python3
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
"""Sample the tablet servers' intents-DB counters, which Prometheus does not scrape.

The conflict collapse has no surviving explanation on the committer's side: read validation is
1-5 ms in every condition, the retry path is never taken, and YugabyteDB reports zero
write-write conflicts. What is left is the write path, and the slow regime differs from the
fast one by two orders of magnitude of in-flight transactions -- ~3M against ~18,000 -- which
is two orders of magnitude of provisional records held in the intents DB.

Every write has to seek the intents DB to check for a conflicting intent, so the size of that
DB is a per-write cost, and RocksDB stalls a writer outright when its memtables back up. Both
are counted here.

Why a separate sampler: the tablet server exposes 682 metric names on :5340, of which 124 are
intentsdb_*, and Prometheus holds none of them -- the collection's scrape config keeps a much
narrower set. Polling the endpoint directly needs no deployment change and no Prometheus
restart, which matters while a measurement is in flight.
"""
import json
import os
import subprocess
import time

# commit1..commit12 are 10.241.64.10 through .21 -- each hosts a tablet server, and three of
# them also host a yb-master, so the range is NOT the .13+ the committer hostnames suggest.
# Probed rather than assumed: the first version polled .13-.24, which silently sampled 9 of 12
# tablet servers plus the monitor at .22 and two machines with nothing on the port.
HOSTS = os.environ.get(
    "FX_TSERVERS",
    ",".join(f"10.241.64.{h}" for h in range(10, 22)),
).split(",")
PORT = os.environ.get("FX_TSERVER_PORT", "5340")
OUT = os.environ.get("FX_INTENTS_OUT", "/data1/logs/intents.jsonl")
INTERVAL = int(os.environ.get("FX_INTENTS_INTERVAL", "30"))

# Counters, so a rate needs two samples; recorded raw and differenced later rather than
# smoothed here, because a stall that lasts one interval is exactly what this is looking for.
WANTED = (
    "intentsdb_rocksdb_stall_micros",
    "intentsdb_rocksdb_number_db_seek",
    "intentsdb_rocksdb_number_db_seek_found",
    "intentsdb_rocksdb_number_reseeks_iteration",
    "intentsdb_rocksdb_db_seek_micros_sum",
    "intentsdb_rocksdb_db_seek_micros_count",
    "intentsdb_rocksdb_compaction_times_micros_sum",
    "intentsdb_rocksdb_bytes_written",
    "intentsdb_rocksdb_bytes_read",
    "regulardb_rocksdb_stall_micros",
    "regulardb_rocksdb_bytes_written",
    "regulardb_rocksdb_compaction_times_micros_sum",
)


def scrape(host):
    """Sum each wanted counter across the tablets on one server, and for ns_0 alone.

    The line format is `name{labels} VALUE TIMESTAMP_MS`, so the value is the SECOND to last
    field. Taking the last one sums timestamps instead, which reads as every counter holding
    the same enormous number -- the first version of this did exactly that.
    """
    cmd = ["curl", "-sk", "--max-time", "20", f"https://{host}:{PORT}/prometheus-metrics"]
    try:
        out = subprocess.run(cmd, capture_output=True, text=True, timeout=30).stdout
    except subprocess.TimeoutExpired:
        return None
    totals, ns0 = {}, {}
    for line in out.splitlines():
        if not line or line[0] == "#":
            continue
        name = line.split("{", 1)[0].split(" ", 1)[0]
        if name not in WANTED:
            continue
        parts = line.rsplit(" ", 2)
        # With a timestamp there are three trailing fields; without one, two.
        raw = parts[1] if len(parts) == 3 and parts[2].isdigit() else parts[-1]
        try:
            value = float(raw)
        except ValueError:
            continue
        totals[name] = totals.get(name, 0.0) + value
        # The table under test, kept apart because the intents DB also carries tx_status and
        # the system namespaces, whose write rates differ from the state table's.
        if 'table_name="ns_0"' in line:
            ns0[name] = ns0.get(name, 0.0) + value
    if not totals:
        return None
    return {"all": totals, "ns_0": ns0}


def main():
    while True:
        row = {"at": time.time(), "hosts": {}, "missing": []}
        agg, agg_ns0 = {}, {}
        for h in HOSTS:
            t = scrape(h)
            if t is None:
                row["missing"].append(h)
                continue
            row["hosts"][h] = t["all"]
            for k, v in t["all"].items():
                agg[k] = agg.get(k, 0.0) + v
            for k, v in t["ns_0"].items():
                agg_ns0[k] = agg_ns0.get(k, 0.0) + v
        row["total"] = agg
        row["total_ns_0"] = agg_ns0
        if not agg:
            row["note"] = "no tablet server answered (bring-up, redeploy, or all down)"
        with open(OUT, "a") as f:
            f.write(json.dumps(row) + "\n")
        time.sleep(INTERVAL)


if __name__ == "__main__":
    main()
