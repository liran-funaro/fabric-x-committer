<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Evaluation work items

Tasks only. Findings live in `cluster-optimization-log.md`, the tunings worth keeping in
`optimization-summary.md`, and results in `evaluation.tex`. What is true only right now — what is
running, which clock a log uses — is in `session-handoff.md`.

One arm can be up at a time and switching arms is a full bring-up (~10 min), so the two arms are two
batches. `RUNNING.md` has how to run them.

## Queue

Order is by what each batch decides for the document, not by what is interesting. Completed batches and
their results are in `cluster-optimization-log.md` sections 9-11.

| # | batch | what it decides | state |
|---|---|---|---|
| 3b | `nosplit250-rep1..3` | Whether 250,000 holds at twelve tablets from a fresh deployment. Three bring-ups, three readings. [ctx](#3b-the-250000-repeats) | queued, ahead of the A/B |
| 4 | `ladder8tab` | Finish the pinned-vs-splitting A/B at identical rates; `hold8nosplit` is done. | queued |
| 5 | size sweep | 300 B re-measured, 3 KiB added, holds for 1 KiB and 4 KiB. Figure 5 and Table 1 have no current data. | queued |
| 6 | `soak` + `ds5age` | Whether the no-split advantage survives the table crossing the 10 GiB split threshold. | queued |
| 7 | `tabhold` | The tablet axis at two fixed rates, 12/24/48/64. Splitting pinned via `cluster-nosplitting.yaml`. | queued |
| 8 | `ds1`…`ds0001` | Conflict share 1% down to 0.001%. | queued |
| 9 | `vc9` | Nine validator--committers on the nine non-master database nodes. | queued |
| — | `insert_ns` A/B | Whether the rewrite makes the 120-way split meet the bound, and whether the conflict-free path regressed. [ctx](#2g-the-insert_ns-rewrite) | after 1-9 |

## Committer arm (`inventory/cluster.yaml`)

| # | Task | ids | State |
|---|---|---|---|
| 2b | Conflict-share sweep at 1%, 0.1%, 0.01%. | `ds1`…`ds0001` | queued (batch 8) |
| 2d | One `EXPLAIN (ANALYZE, DIST)` at the real batch width, reading `Storage Read Requests`. `fx-explain-insert.sql` runs on its own 120-tablet table, so it needs only a measurement-free gap. | — | ready, cheap |
| 2l | **Quantify read validation on a conflict workload.** `vcservice_database_tx_batch_validation_latency_seconds` reads empty in every row ever recorded, so the one cost that distinguishes a conflict workload from a clean one has never been measured. Seconds there means the insert queues behind per-key reads; milliseconds means the cost is in the write path. [ctx](#2l-the-unmeasured-validation-cost) | new | ready, needs a conflict run |
| 2f | Re-run the tablet axis with automatic splitting disabled. | `tabhold*` | queued (batch 7) |
| 2g | **Order is free: the swap is reversible.** Both binaries are staged (`/data1/bin-stage` baseline, `/data1/bin-stage-rewrite`), and every bring-up recreates namespaces, so it can run before or after the characterisation batches. `fx-plan-24b-ab.sh` swaps, gates on the binary AND on `pg_get_functiondef`, runs both sides, and restores the baseline. Measure the `insert_ns` rewrite: 5% at the 120-way split, plus the conflict-free hold as a regression check. **`eval/workspace` already carries the rewrite, so a baseline bring-up must build with `d44ef3a4` reverted.** [ctx](#2g-the-insert_ns-rewrite) | new | ready |
| 2h | Close the no-pre-split **conflict-free** ceiling with a ladder at twelve tablets. The kept-pre-split case currently rests on a one-sided bound. [ctx](#2h-the-no-pre-split-conflict-free-ceiling) | new | ready, needs the arm |
| 2i | ~~Settle whether 250,000 holds at twelve tablets.~~ **Done:** 2 of 3 MET at 242-244 ms; the configuration is bistable, and the bridge rule is weakened to a probability. See the log. | `9c-nosplit250-rep1..3` | done |
| 2k | **Does the graph's admission cap participate in the slow regime?** Repeat 250,000 at twelve tablets with `committer_coordinator_dep_graph_wait_tx_limit` raised 40x. The slow regime pins the graph at exactly 500,000 while the fast one sits at 18,000; the existing 20M test ruled the cap out at the 120-way split, a different regime. [ctx](#2k-the-graph-cap-at-twelve-tablets) | new | ready, cheap |

## End-to-end arm (`inventory/cluster-orderer.yaml`)

| # | Task | ids | State |
|---|---|---|---|
| 3 | Re-run the size sweep's lost rows: 300 B, 1 KiB, 3 KiB, 4 KiB. [ctx](#3-5-the-size-sweep-rows-to-re-run) | `e2e-size*` | queued (batch 5) |
| 4 | Get a valid 4 KiB hold, or record that it has a probe and no hold. Its last hold read `finished=0` at a rate a probe had just passed. | `e2e-size4096` | queued (batch 5) |
| 5 | Name the ~415 MB/s byte-throughput ceiling between 2 and 3 KiB, with disk utilisation ruled out at 44%. | — | open, unowned |

## Evaluation document

| # | Task | Blocked on |
|---|---|---|
| 7 | Draw figure 1c's collapsed bars from a point that delivered its rate. | nothing — batch 3's rows exist |
| 8 | Decide whether the deeper generator block buffer joins the tuned setup, and so whether figure 2 carries that ladder as a series. | a decision, not a run |
| 9 | Update figure 5, Table 1 and the size section. | batch 5 |
| 10 | Re-run or withdraw the eight-tablet paragraph added in `7b87e7f7`, whose rows were lost with the monitor. [ctx](#10-the-eight-tablet-paragraph) | batch 4 |
| 11 | Sync the results file into the repo at every batch boundary, from the chain script. Four hours of rows were lost to a reprovision because they were only on the monitor. | — |
| 12 | Revise the twelve-tablet conflicting claim, which still says 150,000. 250,000 is now retired on two of three fresh deployments at 242-244 ms, but the configuration is bistable at that rate, so the number alone would mislead either way. [ctx](#12-what-to-claim-for-a-bistable-rate) | 2k, which decides whether the cap is part of it |

## Not scheduled

- **Separate the load generator from the pipeline at ~500,000 tps.** It runs at 77% against the busiest
  validator--committer's 81%, so every figure near the knee bounds the pair. Needs a second generator and
  there is no spare machine.

## Task context

Detail that does not fit a table row. One heading per task; the row links here.

### 2g: the `insert_ns` rewrite

`utils/statedb/create_namespace_tmpl.sql` now uses `ON CONFLICT (key) DO NOTHING ... RETURNING key`, with the
violating set computed in the same statement as `_keys EXCEPT ALL inserted` (`d44ef3a4`; sanctioned in
session, provenance in `cluster-optimization-log.md` section 13). No Go changed and no contract changed. It is
built but **not deployed**, so nothing measured so far is affected.

**What to run**, A/B on the same day, fresh deployment each:

1. 5% double spends at the 120-way pre-split — the configuration that currently never meets the bound at any
   offered rate. Does it now?
2. The conflict-free 518,000 hold, as a regression check: the common path now materialises a `RETURNING` set
   and compares cardinalities where it returned `'{}'` after a bare INSERT, and it runs ~3,400 times a second.

Read `db_insert`, `db_insert_per_commit` and the p99/mean pair on both.

**Three pre-checks gate the measurement**, because a null result is otherwise indistinguishable from a failed
deploy:

1. `EXPLAIN (ANALYZE, DIST)` at the real batch width, reading `Storage Read Requests` — task 2d's check.
   YugabyteDB must read something to detect a primary-key conflict; if it reads per key the cost moved rather
   than went.
2. `pg_get_functiondef` on `insert_ns_%` off the running database, grepped for `ON CONFLICT`.
   `CREATE OR REPLACE` is not a live upgrade path, so a namespace already created keeps the old function.
3. `strings` on the staged binary at `out/control-node/bin/Linux/x86_64/` immediately before the batch, not
   once beforehand — an ordinary bring-up rewrites that path. Discriminate on **`EXCEPT ALL`: 0 = old, 2 = rewrite** --
   the only present/absent test. `unique_violation` reads 2 against 1, not 2 against 0, because
   `init_database_tmpl.sql` has its own handler. Do *not* use `ON CONFLICT`: it reads 3 in both binaries.

### 2h: the no-pre-split conflict-free ceiling

The section that keeps the 120-way split divided 518,000 by 213,091, and 213,091 is a single MET probe never
pushed higher. Corrected to a one-sided bound in `74dd76e3`; closing it needs a conflict-free ladder at twelve
tablets. Until it runs, the cost of dropping the pre-split on a clean workload is unmeasured, which is the
open half of the conflict recommendation in `optimization-summary.md`.

### 3b: the 250,000 repeats

`nosplithi` read 250,000 as MET, then its bridge rung read the same rate as MISSED after a genuine redeploy.
Retracted from the document in `b054a57f`; the section now claims 150,000. Three fresh-deployment repeats,
each with its own bring-up, settle it. `fx-leader-skew.sh` samples leader placement every 60 s throughout, the
one candidate that was never being recorded. A prediction for the outcome is in
`cluster-optimization-log.md` section 10.

### 3-5: the size sweep rows to re-run

The rows for 300 B, 1 KiB, 3 KiB and 4 KiB were lost with the monitor; their *values* survive in
`cluster-optimization-log.md` section 11, but they cannot be re-plotted. Figure 5 and Table 1 need rows, not
values.

### 10: the eight-tablet paragraph

The paragraph added in `7b87e7f7` ("Eight tablets is not enough for this workload; twelve is") rests on
`hold8nosplit` rungs 2-8 and all of `ladder8tab`, both lost with the monitor. Either re-run both or mark the
paragraph pending — a claim whose rows are absent should not sit in the document unmarked.

### 2k: the graph cap at twelve tablets

The 20,000,000 test that ruled the cap out was taken at the 120-way pre-split, where capacity is ~20,300
and the insert costs 1.7 s -- there the graph is full because in-flight equals throughput times latency,
which is an effect. At twelve tablets and 250,000 offered the insert is 17 ms in the fast regime, and the
slow regime pins the graph at exactly its 500,000 limit while the fast one sits at 16,000-19,000. Same
rate, same layout, no overlap. Raise the limit 40x and repeat 250,000 three times: if it retires the rate
every time, the cap participates in the collapse; if it still flips, it does not and the bistability is
elsewhere.

### 12: what to claim for a bistable rate

`b054a57f` pulled this back to 150,000 when 250,000 failed to reproduce, and that was right about what
was established. Batch 3b now has 250,000 retired in full twice at 242-244 ms and missed once at 29.2 s,
with no continuum between the two -- so neither "250,000" nor "150,000" describes the configuration on
its own. What the publication can carry depends on 2k: if raising the graph's admission limit removes the
slow regime, the claim is 250,000 with a named setting; if it does not, the honest claim is a rate that
holds about two times in three, which belongs in the text as a property of the configuration rather than
as a ceiling. Do not update the figure until that is decided -- an axis cannot show a bimodal outcome,
and plotting the mean of two regimes would invent a rate the deployment never runs at.

### 2l: the unmeasured validation cost

Every conflict-handling explanation is now refuted: the committer's retry path (attempts 1.0), the
`insert_ns` violating-key lookup (never entered at gap 300,000), and the database's own conflict
resolution (`transaction_conflicts` 0/s across 82 samples, resolution path never entered). What remains
is that a back-referenced workload issues multi-key lookups on keys that *hit*, which is the read-batching
cliff, and that the insert queues behind them rather than being slow itself.

`vcservice_database_tx_batch_validation_latency_seconds` is already in `fx-graph-sampler.py`'s queries and
already exported, but every recorded row has it empty, so nobody has looked. Read it beside `db_insert` on
a conflict run at both tablet counts. Seconds of validation with seconds of insert supports the queueing
account; milliseconds of validation refutes it and puts the cost inside the write path.
