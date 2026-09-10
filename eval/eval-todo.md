<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Experiments still to run

Status at 2026-09-10 14:00. One arm can be up at a time and switching arms is a full bring-up
(~10 min), so the two tables are the two batches. `RUNNING.md` has how to run them.

## Committer arm (`inventory/cluster.yaml`)

| # | Experiment | ids | Status |
|---|---|---|---|
| 1 | **Double spends at the scaled reference gap.** 5, 10, 20, 30% over a 225,000-transaction gap, which matches the paper's in-flight share (5.9%) rather than its gap. Fills figure 1c. | `9c-ds5` … `9c-ds30` | **running** since 13:46, ~3 h |
| 2 | **Small-block ceiling above 380,000 tps.** The old 430,000 was the generator's own block preparation. Needs `ae27afe4` (`eval/fast-block-prepare`) merged onto the eval branch, `fast-block-prepare` and `prepare-in-place` on in the inventory, and a rebuilt loadgen — the deployed binary has neither flag. Extends figure 2. | `curve500top` | blocked on that merge |

## End-to-end arm (`inventory/cluster-orderer.yaml`)

| # | Experiment | ids | Status |
|---|---|---|---|
| 3 | **Re-measure 300 B.** Its first probe measured a draining backlog — delivered 492,182 against 480,000 offered — so the search stepped down to 408,000, while the same workload held 499,091 on the shard ladder. The driver now drains before the first probe. Figure 5's leftmost point is ~20% low until this runs. | `e2e-size300` | ready |
| 4 | **3 KiB size point.** The sweep jumps 2 KiB → 4 KiB, which is where it becomes disk-bound, so the knee is unbracketed. | `e2e-size3072` | ready |
| 5 | **Holds for 1 KiB and 4 KiB.** Both are probes: an assembler volume fills in under twenty minutes at those sizes, so a search and a hold do not fit in one deployment. Needs two phases — search, redeploy, hold at the found rate. | `e2e-size1024`, `e2e-size4096` | needs the two-phase runner |

## Not scheduled

- **Separate the load generator from the pipeline at ~500,000 tps.** It runs at 77% against the busiest
  validator--committer's 81%, so every figure near the knee bounds the pair. Needs a second generator and
  there is no spare machine.
- **Why 2 KiB knees at 156,060 tps.** No machine above 54% CPU and the byte rate is three fifths of the
  assembler disk ceiling, so neither explains it.
- **The default tablet split.** Dropped: the paper used the same 120-way split, so it was never the
  variable those runs treated it as.

## Done

Figure 1a/1b under Ed25519 and re-measured under ECDSA (indistinguishable, mean ratio 1.003) · figure 2
both block sizes · figure 3 four and eight shards · figure 4 batch size · figure 5 at 300 B, 512 B,
1 KiB, 2 KiB, 4 KiB · table 2 disk characterisation · the dependency-graph per-key cost · the fill check.
