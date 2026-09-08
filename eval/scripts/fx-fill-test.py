#!/usr/bin/env python3
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
"""
Ask whether the state table's size affects the database's commit latency, using one hold window.

A hold runs 375 s at a fixed rate, so the table grows by the whole window's output while the rate
stays constant -- 194 million rows at the two-read-write knee, and the table roughly doubles inside
the window. That makes a single hold a controlled experiment on fill: rate fixed, fill rising.

The confounded version of this question fitted commit latency against fill across a knee search,
where every probe steps the rate up, so fill and rate rose together and the fit was against both.
This isolates fill.

Reads the last confirmed hold out of figures.jsonl unless given an experiment id, then range-queries
Prometheus across that window. Read-only; runs while the next point is deploying, since the window
has already passed.
"""
import json
import os
import subprocess
import sys

PROM = os.environ.get("FX_PROM", "https://localhost:9090")
FIGURES = os.environ.get("FX_OUT", "/data1/logs/figures.jsonl")

COMMIT_MS = ("1000 * sum(rate(vcservice_database_tx_batch_commit_latency_seconds_sum[1m]))"
             " / sum(rate(vcservice_database_tx_batch_commit_latency_seconds_count[1m]))")
COMMITTED_TOTAL = "sum(loadgen_transaction_committed_total)"
RATE = "sum(rate(loadgen_transaction_committed_total[1m]))"


def query_range(expr, start, end, step=30):
    cmd = ["curl", "-sk", "--max-time", "30", "-G",
           "--data-urlencode", f"query={expr}",
           "--data-urlencode", f"start={start}",
           "--data-urlencode", f"end={end}",
           "--data-urlencode", f"step={step}",
           f"{PROM}/api/v1/query_range"]
    out = subprocess.run(cmd, capture_output=True, text=True, timeout=40).stdout
    try:
        result = json.loads(out)["data"]["result"]
    except (json.JSONDecodeError, KeyError):
        return []
    if not result:
        return []
    return [(float(t), float(v)) for t, v in result[0]["values"] if v not in ("NaN", "+Inf")]


def main():
    # Prometheus is torn down with each deployment, so a hold's window is unreadable once the next
    # point starts. --live reads the last 300 s from now instead, which is how this gets run while
    # the hold it measures is still in flight.
    if sys.argv[1:2] == ["--live"]:
        import time
        end = time.time()
        hold = {"experiment": "live", "limit": 0, "window": 300, "at": end}
        report(hold)
        return
    wanted = sys.argv[1] if len(sys.argv) > 1 else None
    holds = []
    with open(FIGURES) as f:
        for line in f:
            row = json.loads(line)
            if row.get("kind") in ("hold", "curve") and row.get("met"):
                if wanted is None or row["experiment"] == wanted:
                    holds.append(row)
    if not holds:
        print("no confirmed hold to test")
        return
    report(holds[-1])


def report(hold):
    start, end = hold["at"] - hold["window"], hold["at"]
    print(f"{hold['experiment']} at {hold['limit']:,} tps, {hold['window']}s window")

    latency = dict(query_range(COMMIT_MS, start, end))
    committed = dict(query_range(COMMITTED_TOTAL, start, end))
    rate = dict(query_range(RATE, start, end))
    if not latency or not committed:
        print("  no data for that window (Prometheus restarts with each deployment)")
        return

    base = min(committed.values())
    # The first samples of a window are not usable evidence about fill: the rate changed just before
    # it, so a 1-minute average includes the idle period, and warm-up (connection pools filling,
    # tablet leaders settling, page cache populating) runs on the same timescale. Fill is the only
    # one of the three that keeps going for the whole window, so the question is whether the rise
    # continues after the first two minutes, not whether it happens at all.
    print(f"  {'t+s':>5} {'fill Mtx':>9} {'commit ms':>10} {'rate tx/s':>10}   note")
    for t in sorted(latency):
        if t not in committed:
            continue
        note = "warm-up contaminated" if t - start < 120 else ""
        print(f"  {t - start:>5.0f} {committed[t] / 1e6:>9,.1f} {latency[t]:>10,.1f} "
              f"{rate.get(t, float('nan')):>10,.0f}   {note}")
    grew = (max(committed.values()) - base) / 1e6
    settled = {t: v for t, v in latency.items() if t - start >= 120}
    print(f"  fill grew {grew:,.1f} M during the window")
    if len(settled) >= 2:
        ts = sorted(settled)
        first, last = settled[ts[0]], settled[ts[-1]]
        span = (max(committed.values()) - committed.get(ts[0], base)) / 1e6
        print(f"  after warm-up ({ts[0] - start:.0f}s on): commit latency {first:,.1f} -> "
              f"{last:,.1f} ms while fill grew a further {span:,.1f} M "
              f"({(last / first - 1) * 100:+.0f}%)")


if __name__ == "__main__":
    main()
