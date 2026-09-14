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
| 1 | ~~Every 500-transaction-block measurement, again, with the fast block producer.~~ **Done, null result**: 528,545 tps at 10,000-transaction blocks against 531,455 before, 380,673 at 500 against 379,764. Preparation was not the ceiling. It also answers 1b: the large-block ladder did not move, so figure 1 was never generator-bound. The buffer was: 430,145 tps with a 2,000-block buffer. Original text below.<br><br>**Every 500-transaction-block measurement, again, with the fast block producer.** The old ladder measured the generator: its mock orderer prepared blocks on one goroutine at 0.75 ms each, capping it near 850 blocks a second. `fast-block-prepare` is now in the branch, the collection and the staged binary. Benchmarked here: preparation is a *per-transaction* cost, so it capped a transaction rate rather than a block rate — 775,500 tps at 500 a block and 777,500 at 10,000, on one goroutine of this workstation. The fix is 60x at 500 and 1,133x at 10,000. So **both** ladders need re-running, and if the cluster's generator prepares slower than this machine (2.10 GHz there), both were capped by it. | `curve`, `curve500`, `curve500hi`, `curve500top` | **ready, do first** |
| 1b | **Do figures 1a and 1b need re-measuring too?** Their points were taken with the old block producer at 10,000-transaction blocks, where preparation cost 12.1 ms a block — 63% of one goroutine at 518,000 tps and **79% at 653,273**. Close enough to a single-goroutine ceiling to be suspect. #1's 10,000-block ladder answers it: if it now sustains more than 531,455 tps, the whole of figure 1 was generator-bound and needs re-running. | `9a-*`, `9b-*` | decided by 1 |
| 2 | **Why double spends collapse.** Near 20,000 tps at every conflict share, gap and scheme, against 518,000 conflict-free — rate-independent, with the busiest *database* node at 73% CPU, so neither the rate nor the coordinator. **Leading suspect: YugabyteDB's multi-key read batching.** `key = ANY(array)` batches only while tablets x keys-per-lookup stays under ~32,768; above it, one storage read per key. `validateNamespaceReads` passes every read key of a validation batch in one array, unchunked, so keys per lookup is thousands against 120 tablets. It is invisible conflict-free, because inserting fresh keys performs no multi-key lookup — a back-reference is the first thing that does. The same cliff took a blind-write workload from 314,336 to 13,160 tps. Three variants of one 5% point decide it: fewer tablets, a narrower chunk, or the global graph manager. | `9c-ds5-split8`, `9c-ds5-chunk64`, `9c-ds5-gdg` | ready, after 1 |

## End-to-end arm (`inventory/cluster-orderer.yaml`)

| # | Experiment | ids | Status |
|---|---|---|---|
| 3 | **Re-measure 300 B.** Its first probe measured a draining backlog — delivered 492,182 against 480,000 offered — so the search stepped down to 408,000, while the same workload held 499,091 on the shard ladder. The driver now drains before the first probe. Figure 5's leftmost point is ~20% low until this runs. | `e2e-size300` | ready |
| 4 | **3 KiB size point.** The sweep jumps 2 KiB → 4 KiB, which is where it becomes disk-bound, so the knee is unbracketed. | `e2e-size3072` | ready |
| 5 | **Holds for 1 KiB and 4 KiB.** Both are probes today, but the two phases already exist: the driver redeploys before every hold and refuses one that would not fit the volume. Their failures predate both. On a fresh volume a 300 s hold writes 105 GB at 1 KiB and 148 GB at 4 KiB, against ~485 GB — so they fit, and run as part of #3/#4. | `e2e-size1024`, `e2e-size4096` | ready, folded into 3 |
| 6 | ~~The 500-transaction batch ladder, again.~~ **Dropped**: the fast producer is a mock-orderer path, and on this arm the batchers cut the blocks — the generator submits to routers and cuts nothing. | `e2e-curve-small` | n/a |

## Evaluation document

| # | Item | Blocked on |
|---|---|---|
| 7 | **Restore the double-spend section.** Its two paragraphs are commented out, and figure 1c still draws the gap-0 rows — the panel is orphaned until #2 replaces them. | 2 |
| 8 | **Partly done.** The section no longer attributes the small-block ceiling to block preparation, which the re-measurement refuted: with preparation 1,133x cheaper both ladders landed where they were (528,545 and 380,673 tps). What it now says is what was measured — the bound is the generator's outstanding-work buffer, counted in *blocks*, so deepening it twentyfold moves the ceiling from ~400,000 to 430,145 tps at 209 ms. **Left**: whether figure 2 should carry the deeper-buffer ladder as its own series, which needs a decision on whether that buffer becomes part of the tuned setup. | decision |
| 9 | **Update figure 5, Table 1 and the size section** with 300 B re-measured, 3 KiB added, and holds where they exist. | 3, 4, 5 |
| 10 | ~~Name the paper's committer machines.~~ **Done**: the bullet now gives the three instance types and the EBS rating. | — |
| 11 | ~~Mention that the published committer tier spans three regions.~~ **Dropped**: that came from the §6 text the authors say is wrong. Its arms ran on AWS, and whether the committer tier was multi-region there is not something this repo can establish. | — |

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
scaled to the paper's in-flight share · the default-split series removed from figure 1c, since the paper
used the same 120-way split and those runs carried the unscaled gap · figure 2 capped at one second ·
figure 1 ticked every 100k.

Plot fixes: figure 1 ticks every 100k on all three panels · figure 2's y axis capped at the one-second
bound, so a rung with a 5,333 ms median no longer flattens every rate anyone would run into the bottom
twelfth — it is named in the corner note, and the segment leading to it is not drawn, which had put a
vertical line up the plot exactly where the ceiling is read · figure 5 draws its 4 KiB point again and
its legend carries marks rather than prose · "offered but not sustained" appears in a legend only when
such a mark is on the plot.
