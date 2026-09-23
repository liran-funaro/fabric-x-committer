#!/usr/bin/env python3
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
"""Difference the intents-DB counters over a window and report per-second rates.

The sampler records raw counters, deliberately, so a stall confined to one interval is still
visible. That makes every comparison a subtraction: rate = (last - first) / seconds. Reading a
raw counter as a level instead is how the first version of the sampler reported timestamps as
metric values and nobody noticed for three samples.

Usage:
    fx-intents-compare.py intents.jsonl HH:MM-HH:MM [HH:MM-HH:MM ...]

Windows are UTC clock times on the file's own dates; each is labelled in the output. Give two
to compare a fast regime against a slow one, which is the measurement this exists for.
"""
import json
import sys
import datetime

KEYS = (
    "intentsdb_rocksdb_number_db_seek",
    "intentsdb_rocksdb_number_db_seek_found",
    "intentsdb_rocksdb_bytes_read",
    "intentsdb_rocksdb_bytes_written",
    "intentsdb_rocksdb_stall_micros",
    "intentsdb_rocksdb_compaction_times_micros_sum",
    "regulardb_rocksdb_stall_micros",
    "regulardb_rocksdb_bytes_written",
)


def hm(ts):
    return datetime.datetime.utcfromtimestamp(ts).strftime("%H:%M")


def rates(rows, scope):
    """Per-second rate of each counter between the first and last sample given."""
    have = [r for r in rows if r.get(scope)]
    if len(have) < 2:
        return None, 0
    first, last = have[0], have[-1]
    span = last["at"] - first["at"]
    if span <= 0:
        return None, 0
    out = {}
    for k in KEYS:
        a, b = first[scope].get(k), last[scope].get(k)
        if a is None or b is None:
            continue
        # A counter that goes backwards means the tablet server restarted inside the window,
        # so the window spans a redeploy and its rate is meaningless rather than negative.
        out[k] = None if b < a else (b - a) / span
    return out, span


def main():
    path, windows = sys.argv[1], sys.argv[2:]
    rows = [json.loads(l) for l in open(path)]
    for w in windows:
        lo, hi = w.split("-")
        sel = [r for r in rows if lo <= hm(r.get("at", 0)) <= hi]
        for scope in ("total_ns_0", "total"):
            r, span = rates(sel, scope)
            print(f"\n=== {w}  {scope}  ({len(sel)} samples, {span:.0f}s) ===")
            if not r:
                print("    too few samples with data")
                continue
            for k in KEYS:
                if k not in r:
                    continue
                v = r[k]
                print(f"    {k:46s} {'counter reset (redeploy in window)' if v is None else format(v, ',.1f') + ' /s'}")


if __name__ == "__main__":
    main()
