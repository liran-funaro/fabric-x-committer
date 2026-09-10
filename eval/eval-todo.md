<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Experiments still to run

Status at 2026-09-10 17:50. One arm can be up at a time and switching arms is a full bring-up
(~10 min), so the two tables are the two batches. `RUNNING.md` has how to run them.

## Committer arm (`inventory/cluster.yaml`)

| # | Experiment | ids | Status |
|---|---|---|---|
| 1 | **Every 500-transaction-block measurement, again, with the fast block producer.** The old ladder measured the generator: its mock orderer prepared blocks on one goroutine at 0.75 ms each, capping it near 850 blocks a second. `fast-block-prepare` takes preparation to 8.4 us and is now in the branch, the collection and the staged binary. Both block sizes need re-running, since preparation cost 12.1 ms per 10,000-transaction block too. | `curve`, `curve500`, `curve500hi`, `curve500top` | **ready, do first** |
| 2 | **Why double spends collapse.** Every conflict share, at every gap and both schemes, lands near 20,000 tps against 518,000 conflict-free — and the drain never drains: 3.3M transactions in flight, sidecar waiting set pinned at its 500,000 cap, 73% CPU, mean latency 150 s. Rate-independent, so it is not a knee. Full analysis wanted, in this order: (a) **the dependency-graph manager** — this deployment runs the simple manager and the paper ran the global one, so `gdg-*` against `9c-*` at 5% is the first A/B; (b) the back-reference implementation itself — whether an abort re-enters the graph, and what the graph holds per referenced key; (c) tablet hot-spotting from the 1,000,000-key window; (d) whether aborts are retried anywhere. | `9c-ds*`, `gdg-*`, new | blocked on 1 |

## End-to-end arm (`inventory/cluster-orderer.yaml`)

| # | Experiment | ids | Status |
|---|---|---|---|
| 3 | **Re-measure 300 B.** Its first probe measured a draining backlog — delivered 492,182 against 480,000 offered — so the search stepped down to 408,000, while the same workload held 499,091 on the shard ladder. The driver now drains before the first probe. Figure 5's leftmost point is ~20% low until this runs. | `e2e-size300` | ready |
| 4 | **3 KiB size point.** The sweep jumps 2 KiB → 4 KiB, which is where it becomes disk-bound, so the knee is unbracketed. | `e2e-size3072` | ready |
| 5 | **Holds for 1 KiB and 4 KiB.** Both are probes: an assembler volume fills in under twenty minutes at those sizes, so a search and a hold do not fit in one deployment. Needs two phases — search, redeploy, hold at the found rate. | `e2e-size1024`, `e2e-size4096` | needs the two-phase runner |
| 6 | **The 500-transaction batch ladder, again.** Same reason as #1 if the generator was the constraint on this arm too — here the batchers cut the blocks, so check before spending a run. | `e2e-curve-small` | check first |

## Evaluation document

| # | Item | Blocked on |
|---|---|---|
| 7 | **Restore the double-spend section.** Its two paragraphs are commented out, and figure 1c still draws the gap-0 rows — the panel is orphaned until #2 replaces them. | 2 |
| 8 | **Update figure 2 and its section** — the ladder, the 40 ms floor, the 136 ms median at 379,764 tps, and the sentence attributing the ceiling to the generator's block preparation. | 1 |
| 9 | **Update figure 5, Table 1 and the size section** with 300 B re-measured, 3 KiB added, and holds where they exist. | 3, 4, 5 |
| 10 | **Say what the paper's committer machines were** now that the setup is known (AWS `c6id.8xlarge` per validator-committer with its database node, `c5a.8xlarge` verifiers, `c6id.16xlarge` coordinator/sidecar/loadgen, EBS gp). Currently one bullet; it bears on the per-key hypothesis. | — |
| 11 | **Mention that the published committer tier spans three regions** where this one is in one datacentre. It makes their per-key result more striking, not less. | — |

## Not scheduled

- **Separate the load generator from the pipeline at ~500,000 tps.** It runs at 77% against the busiest
  validator--committer's 81%, so every figure near the knee bounds the pair. Needs a second generator and
  there is no spare machine.
- **Why 2 KiB knees at 156,060 tps.** No machine above 54% CPU and the byte rate is three fifths of the
  assembler disk ceiling, so neither explains it.
- **The default tablet split.** Dropped: the paper used the same 120-way split, so it was never the
  variable those runs treated it as.

## Done

Figure 1a/1b under ECDSA, which the document now draws · figure 2 both block sizes (superseded by #1) ·
figure 3 four and eight shards · figure 4 batch size · figure 5 at 300 B, 512 B, 1 KiB, 2 KiB, 4 KiB ·
table 2 disk characterisation · the dependency-graph per-key cost · the fill check · the reference gap
scaled to the paper's in-flight share.

Plot fixes: figure 1 ticks every 100k on all three panels · figure 2's y axis capped at the one-second
bound, so a rung with a 5,333 ms median no longer flattens every rate anyone would run into the bottom
twelfth — it is named in the corner note, and the segment leading to it is not drawn, which had put a
vertical line up the plot exactly where the ceiling is read · figure 5 draws its 4 KiB point again and
its legend carries marks rather than prose · "offered but not sustained" appears in a legend only when
such a mark is on the plot.
