#!/usr/bin/env python3
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
"""
Report the dependency graph's state during each confirmed hold.

Joins the graph sampler's 30 s samples (graph.jsonl) to the driver's reported points
(figures.jsonl) by timestamp: a hold's window is [at - window, at], so the samples inside it are
the ones taken while that rate was being held. Answers the question the paper's Figure 9a raises
-- whether the graph is what makes large transactions expensive -- with the queue depths and the
per-transaction work at each size, rather than by assuming it.
"""
import json
import sys

figures = sys.argv[1] if len(sys.argv) > 1 else "figures.jsonl"
graph = sys.argv[2] if len(sys.argv) > 2 else "graph.jsonl"

holds = [json.loads(line) for line in open(figures)]
holds = [h for h in holds if h.get("kind") in ("hold", "curve") and h.get("met")]
samples = [json.loads(line) for line in open(graph)]

FIELDS = ("ctor_lock_util", "proc_lock_util", "construct_util", "add_batch_util",
          "detector_util", "graph_size", "in_queue", "dependent_queue", "tx_processed")


def mean(values):
    values = [v for v in values if v is not None]
    return sum(values) / len(values) if values else None


print(f"{'point':<12} {'committed':>10} {'graph tx/s':>11} {'in queue':>9} {'dep queue':>10} "
      f"{'graph size':>11} {'ctor lock':>10} {'proc lock':>10} {'construct':>10}")
for h in holds:
    window = [s for s in samples if h["at"] - h["window"] <= s["at"] <= h["at"] + 5]
    if not window:
        continue
    avg = {f: mean([s.get(f) for s in window]) for f in FIELDS}

    def fmt(name, digits=0):
        v = avg[name]
        return "-" if v is None else f"{v:,.{digits}f}"

    label = h["experiment"]
    if h["kind"] == "curve":
        label = f"{h['limit'] // 1000}k"
    print(f"{label:<12} {(h.get('committed') or 0):>10,.0f} {fmt('tx_processed'):>11} "
          f"{fmt('in_queue'):>9} {fmt('dependent_queue'):>10} {fmt('graph_size'):>11} "
          f"{fmt('ctor_lock_util', 3):>10} {fmt('proc_lock_util', 3):>10} "
          f"{fmt('construct_util', 3):>10}")
