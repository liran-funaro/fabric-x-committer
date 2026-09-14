<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Evaluation work items

Tasks only. Findings live in `cluster-optimization-log.md`, the tunings worth keeping in
`optimization-summary.md`, and results in `evaluation.tex`.

One arm can be up at a time and switching arms is a full bring-up (~10 min), so the two arms are two
batches. `RUNNING.md` has how to run them.

## Queue

Running unattended from `/data1/logs/fx-plan-14q-lf.sh`, in this order.

| # | batch | what it decides | state |
|---|---|---|---|
| 1 | `nosplit` | Whether a conflicting workload has any sub-second operating point, at 5/10/20/30%. | 5% and 10% done, 20% running |
| 2 | `ladder8tab`, `hold8` | The 8-tablet anomaly, and whether the failure-path cost is per-tablet or per-key-per-tablet. | queued |
| 3 | `ladderlow` | Whether the 120-way split misses the bound at a *sustainable* rate. Fills figure 1c. | queued |
| 4 | `tabhold` | The tablet axis at one fixed rate. **Run with automatic splitting disabled**, or the rows are starting values. | queued |
| 5 | `ds1`…`ds0001` | Conflict share 1% down to 0.001%. | queued |
| 6 | `vc9` | Nine validator--committers on the nine non-master database nodes. | queued |
| 7 | size sweep | 300 B re-measured, 3 KiB added, holds for 1 KiB and 4 KiB. Own arm, so it goes last. | queued |

## Committer arm (`inventory/cluster.yaml`)

| # | Task | ids | State |
|---|---|---|---|
| 1 | Re-run every 500-transaction-block measurement with the fast block producer. | `curve*` | done, null result |
| 2 | Find why double spends collapse. | `9c-ds5-*` | done |
| 2a | Tablet sweep at 5% conflicts, to fit a cost law. | `9c-ds5-tab*` | closed, no law identifiable |
| 2b | Conflict-share sweep at 1%, 0.1%, 0.01%. | `ds1`…`ds0001` | queued (batch 5) |
| 2c | Add `db_insert` to the driver's `QUERIES`. | — | done |
| 2d | One `EXPLAIN (ANALYZE, DIST)` at the real batch width, reading `Storage Read Requests`, to close the `chunk64` loose end. | — | open, unowned |
| 2e | Add a gauge for `SimpleManager.depFreeTxBatches`. | — | open, needs code |
| 2f | Re-run the tablet axis with automatic splitting disabled. | `tabhold*` | queued (batch 4) |

## End-to-end arm (`inventory/cluster-orderer.yaml`)

| # | Task | ids | State |
|---|---|---|---|
| 3 | Re-measure 300 B. Its first probe measured a draining backlog; the driver now drains first. | `e2e-size300` | queued (batch 7) |
| 4 | Add the 3 KiB size point, to bracket the disk-bound knee between 2 and 4 KiB. | `e2e-size3072` | queued (batch 7) |
| 5 | Holds for 1 KiB and 4 KiB. Both fit a fresh volume; runs with #3/#4. | `e2e-size1024`, `e2e-size4096` | queued (batch 7) |
| 6 | Re-run the 500-transaction batch ladder. | `e2e-curve-small` | dropped, not applicable to this arm |

## Evaluation document

| # | Task | Blocked on |
|---|---|---|
| 7 | Draw figure 1c's collapsed bars from a point that delivered its rate. | batch 3 |
| 8 | Decide whether the deeper generator block buffer joins the tuned setup, and so whether figure 2 carries that ladder as a series. | a decision, not a run |
| 9 | Update figure 5, Table 1 and the size section. | batches 7 (#3, #4, #5) |
| 10 | Re-run the `split0` conflict series at the documented reference gap before it shares an axis with current numbers. | batch 1 supersedes it |

## Not scheduled

- **Separate the load generator from the pipeline at ~500,000 tps.** It runs at 77% against the busiest
  validator--committer's 81%, so every figure near the knee bounds the pair. Needs a second generator and
  there is no spare machine.
- **Why 2 KiB knees at 156,060 tps.** No machine above 54% CPU and the byte rate is three fifths of the
  assembler disk ceiling, so neither explains it.

## Done

Figure 1a/1b under ECDSA · figure 2 both block sizes · figure 3 four and eight shards · figure 4 batch size ·
figure 5 at 300 B, 512 B, 1 KiB, 2 KiB, 4 KiB · table 2 disk characterisation · the dependency-graph per-key
cost · the table-fill check · the reference gap scaled to the paper's in-flight share · the paper's committer
machine types.
