<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Evaluation work items

Status 2026-09-14 09:20; the double-spend collapse is solved, see below. One arm can be up at a time and switching arms is a full bring-up
(~10 min), so the two tables are the two batches. `RUNNING.md` has how to run them.

## Solved: why double spends collapse to ~20,000 tps

A back-reference puts an existing key in a batch's new-writes. `insert_ns` inserts the batch blind and
returns on success — **no lookup at all** — but on `unique_violation` its handler runs
`key = ANY(_keys)` over *every* key in the batch. At ~1,000 keys against 120 tablets that is 120,000,
past YugabyteDB's ~32,768 batching threshold, so it degrades to one storage read per key. The batch then
rolls back and the Go retry loop re-runs it.

| evidence | conflict-free | 5% conflicts, 120 tablets | 5% conflicts, 8 tablets |
|---|---|---|---|
| throughput | 518,727 tps | 21,273 | **181,091** |
| busiest host CPU | 80% | 71% | 21% |
| CPU per transaction | 99 µs | **2,133 µs** (21×) | 75 µs |

Plus, from the VC's own counters: insert latency **1.72 s per call at 1.91 calls per commit** — the retry
loop, first attempt violating and second succeeding.

At 8 tablets a conflicting workload shows **no visible CPU penalty** — 75 µs against 99 conflict-free is
the same order. It is not evidence that conflicts are *cheaper*: that point ran at 181,091 tps against
518,727, a third of the rate, so it also carries less queueing per transaction. The defensible claim is
that the 21× penalty is gone, not that the sign reverses.

**Why it is invisible without conflicts**, which is what made this hard to find: nothing performs a
multi-key lookup when every key is new. One conflicting key makes the whole batch perform one.

**Why the database looked innocent for two days**: the driver samples
`vcservice_database_tx_batch_commit_latency` — 22.9 ms — a span that *excludes* the insert.
`..._commit_insert_new_key_with_value_latency` is 1.72 s. Two spans for one batch, 75× apart.

**The fix**: `INSERT ... ON CONFLICT DO NOTHING ... RETURNING` in place of the exception handler, which
removes both the full-batch lookup and the rollback. Violating keys become requested-minus-returned, so
the return value inverts and its unit tests change with it. Needs sanction — it is the commit path.

**Instrument defects this hunt exposed**, all three of which produced plausible numbers:

1. `db_commit` samples a span that *excludes* the insert — 22.9 ms against 1.72 s for the same batch. Three
   sessions independently concluded "the database is healthy" from it. `db_insert` and
   `db_insert_per_commit` are in the driver now.
2. `FX_DRAIN_RATE` defaults to **20,000**, and the conflict workload's capacity is **20,235**. So the drain
   parked the generator at the rate the pipeline could just retire, the backlog never fell, and the
   give-up test fired at once — the log reads "draining: 1,830,000 in flight" and then *rises* to 3.34M.
   Every latency from ladder5m rung 2 onward is the age of that backlog, not a cost of the workload:
   23,004 tps × 148.8 s = 3.42M, which is the reported in-flight figure. Throughput survives this (a
   saturated pipeline retires at capacity whatever the queue depth) but latency does not. `drain()` now
   parks at a tenth of what the pipeline is retiring rather than at a fixed rate.
3. The sampler `cd`'d into the Prometheus config directory once at startup; a bring-up deleted and
   recreated it, leaving the process on an unlinked inode and the log full of dashes for 45 minutes
   across live measurement.

**Refuted along the way**, each on evidence: the nil-version insert path (the published generator shared
it), the tablet pre-split as a *difference* from the published run (it had one too), the reference gap
versus the graph window (the abort rate matches the configured share exactly, so references do land on
committed keys), the dependency-graph manager choice (both managers collapse identically), and the graph's
admission limit as a cliff — `waiting-txs-limit: 5000000` is applied and the population still pins at
500,000, because the *sidecar's* 500,000 caps what can be outstanding at all.

## Committer arm (`inventory/cluster.yaml`)

| # | Experiment | ids | Status |
|---|---|---|---|
| 1 | ~~Every 500-transaction-block measurement, again, with the fast block producer.~~ **Done, null result**: 528,545 tps at 10,000-transaction blocks against 531,455 before, 380,673 at 500 against 379,764. Preparation was not the ceiling. It also answers 1b: the large-block ladder did not move, so figure 1 was never generator-bound. The buffer was: 430,145 tps with a 2,000-block buffer. Original text below.<br><br>**Every 500-transaction-block measurement, again, with the fast block producer.** The old ladder measured the generator: its mock orderer prepared blocks on one goroutine at 0.75 ms each, capping it near 850 blocks a second. `fast-block-prepare` is now in the branch, the collection and the staged binary. Benchmarked here: preparation is a *per-transaction* cost, so it capped a transaction rate rather than a block rate — 775,500 tps at 500 a block and 777,500 at 10,000, on one goroutine of this workstation. The fix is 60x at 500 and 1,133x at 10,000. So **both** ladders need re-running, and if the cluster's generator prepares slower than this machine (2.10 GHz there), both were capped by it. | `curve`, `curve500`, `curve500hi`, `curve500top` | **ready, do first** |
| 1b | **Do figures 1a and 1b need re-measuring too?** Their points were taken with the old block producer at 10,000-transaction blocks, where preparation cost 12.1 ms a block — 63% of one goroutine at 518,000 tps and **79% at 653,273**. Close enough to a single-goroutine ceiling to be suspect. #1's 10,000-block ladder answers it: if it now sustains more than 531,455 tps, the whole of figure 1 was generator-bound and needs re-running. | `9a-*`, `9b-*` | **no — answered by 1** |
| 2 | ~~Why double spends collapse.~~ **SOLVED — see the section below.** It is `insert_ns`'s exception handler: one conflicting key makes the whole batch look up every key it holds, which past YugabyteDB's batching threshold costs 1.72 s and a rollback. | `9c-ds5-*` | done |
| 2a | **Tablet sweep at 5% conflicts, plotted against `tablets × keys` rather than tablets.** The width that reaches `insert_ns` is not the graph's 500-transaction chunk — the VC re-batches, and the measured median is 152 transactions (~304 keys, n=14,682), which puts the crossing near **108** tablets, not 32. So 8/16/32/64 all sit under the threshold at that width and a sweep on tablets alone can show nothing while looking like a refutation. Worse, the width falls from ~300 to ~125 as the collapse sets in, so it moves with what is being measured. Record keys-per-insert at each point (`tx_per_db_batch` is already derived) and the sweep interprets itself either way.<br><br>**Two width measurements disagree and the crossing moves with them.** The median 152 tx (~304 keys, crossing ~108 tablets) is taken across the run; two 40-second windows on one VC's own counters, at table sizes 61% apart, both give **172–175 tx (~349 keys, crossing ~93)** and agree with each other to 2%. The likely reconciliation is that the median spans the ramp into collapse, where width genuinely falls, while the steady state is 172–175 — which is also why 2a saw ~300 falling to ~125 and a steady-state sample does not. Either way the instruction stands and matters more than the number: **sample width in-window at each tablet count**, because width follows the downstream service rate, so a tablet change that moves throughput moves width and therefore the predicted edge. Assuming a fixed 349 or 304 can make a holding model look falsified. | new | ready |
| 2b | **Conflict-share sweep**: 1%, 0.1%, 0.01%. P(a ~500-transaction batch holds a conflict) is 94%, 25%, 2.8%, so throughput should climb steeply below 1%. Tests "per batch, not per conflict" with no code change. | new | ready |
| 2c | **Add `db_insert` to the driver's QUERIES** (`vcservice_database_tx_batch_commit_insert_new_key_with_value_latency_seconds`). Sampling only `db_commit` is what hid this for two days — 22.9 ms against 1.72 s for the same batch. | — | `fx-figures.py` owner |
| 2d | **`chunk64` loose end**: 64 transactions is ~128 keys, so 128 × 120 = 15,360 is *under* the threshold and should have been fast, yet it gave 46,182 tps. `vcservice_batcher_input_queue_size` exists, so the VC probably re-batches and the graph chunk does not control the insert's key count. One `EXPLAIN (ANALYZE, DIST)` at the real batch width, reading `Storage Read Requests`, settles it. | — | either |
| 2e | **A gauge for `SimpleManager.depFreeTxBatches`.** ~497,000 transactions are dep-free, released, and in none of the seven queues, so they can only be in that slice — which the code calls "deliberately unbounded" and which has no metric. | — | code |

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
| 7 | **Figure 1c and its section.** No valid double-spend measurement exists at the deployment's own tablet count — every run but the 8-tablet one sits inside the collapsed regime, so the panel cannot be compared with the published Figure 9c. Either report the 8-tablet point and say plainly why the 120-tablet one is not a property of the pipeline, or wait for the `insert_ns` fix. | 2a, or the fix |
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
