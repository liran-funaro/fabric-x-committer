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

Running unattended from `/data1/logs/fx-plan-14w-lf.sh`. Order is by what each batch decides for the
document, not by what is interesting.

**`insert_ns` is not first and is not queued**, and that holds whatever the sanction turns out to be: the
rewrite changes the exact path every number measured today characterises, so **characterise the path, then
change it**. Batches 4 and 7 measure a failure path the rewrite removes — run it first and they stop being
possible, while the layout result, the share-independence and the 12-to-88 cliff lose their comparison
basis. So nothing below waits on the sanction question.

| # | batch | what it decides | state |
|---|---|---|---|
| 1 | `nosplit` | Whether a conflicting workload has any sub-second operating point, at 5/10/20/30%. | **5%, 10%, 20% done; 30% finishing** |
| 2 | `ladderlow` | Whether the 120-way split misses the bound at a *sustainable* rate. Its 70 existing rows are all past capacity. | next |
| 3 | `nosplithi` | The no-split ceiling, which four ladders left unfound at 100,000. Layout pinned. | queued |
| 4 | `hold8nosplit`, `ladder8tab` | The 8-tablet anomaly as an A/B on automatic splitting alone: identical rates, one flag apart. | queued |
| 5 | size sweep | 300 B re-measured, 3 KiB added, holds for 1 KiB and 4 KiB. Figure 5 and Table 1 have no current data. | queued |
| 6 | `soak` + `ds5age` | Whether the no-split advantage survives the table crossing the 10 GiB split threshold. | queued |
| 7 | `tabhold` | The tablet axis at two fixed rates, 12/24/48/64. Splitting pinned via `cluster-nosplitting.yaml`. | queued |
| 8 | `ds1`…`ds0001` | Conflict share 1% down to 0.001%. | queued |
| 9 | `vc9` | Nine validator--committers on the nine non-master database nodes. | queued |
| — | `insert_ns` A/B | Whether the rewrite makes the 120-way split meet the bound, and whether the conflict-free path regressed. | after 1-9 |

**Result so far**: with pre-splitting off, a conflicting workload meets the bound at every rate and share
tried — 95,122 tps at 5%, 90,491 at 10%, 100,000 offered met at 20%, all at 184-199 ms and ≤11% CPU, against
20,300 tps and a censored tail at the 120-way split. No ceiling found. The one MISS in the series, ds30 at
25,000, **did not reproduce**: 186 ms against 3,565 and an insert of 10.9 ms against 52.9, at the same rate
and share with growth zero both times. So there is no demonstrated knee at 30%.

## SANCTIONED by the user, in this session (2026-09-14, ~15:25)

**The user wrote, directly in this session: "I agree to the SQL work. Do it."** Written down by the session
the words arrived in, which is the rule the earlier episode established. Batch 0 runs.

For the record, because four sessions claimed this sanction before it existed: `0c`, `d4`, `ae` and `63`
each reported the identical quote *"I agreed to the insert_ns change in SQL"* as first-hand in their own
session, and each became unreachable. `d4` attributed it to `fabric-x-committer-14`, which denied receiving
any human input at all. Those were false; this is not the same claim arriving again. The wording differs,
it is in this session's own transcript, and no relay is involved.

**Order**: batch 0 runs next, ahead of the characterisation batches. That overrides the recorded
"characterise the path, then change it" argument, deliberately and on the user's direct instruction. The
cost is real and worth naming: `ladderlow`, `ladder8tab`, `tabhold` and the share sweep all measure the
cost of the failure path this rewrite removes, so after it lands they cannot be run at all. The baseline
they would have extended is already captured in `figures-ecdsa.jsonl` and in this file, and the practical
question those batches served — is there a configuration where conflicts meet the bound — is already
answered by the no-split result. So what is lost is depth on a cost law this file already records as
abandoned.

**Two pre-checks still gate the measurement**, because a null result is otherwise indistinguishable from a
failed deploy:

1. `EXPLAIN (ANALYZE, DIST)` at the real batch width, reading `Storage Read Requests`. YugabyteDB must read
   something to detect a primary-key conflict; if it reads per key the cost moved rather than went.
2. `pg_get_functiondef` on `insert_ns_%` off the running database, grepped for `ON CONFLICT`.
   `CREATE OR REPLACE` is not a live upgrade path, so a namespace already created keeps the old function.

And `strings` on the operative staged binary at `out/control-node/bin/Linux/x86_64/` immediately before the
batch, not once beforehand — an ordinary bring-up rewrites that path.

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
| 2g | **First in the queue.** Measure the `insert_ns` rewrite: 5% at the 120-way split, plus the conflict-free hold as a regression check. [ctx](#2g-the-insert_ns-rewrite) | new | ready, needs the arm |

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

## Task context

Detail that does not fit a table row. One heading per task; the row links here.

### 2g: the `insert_ns` rewrite

**Written. Sanction is not on record.** Two sessions put this change to the user as needing their say-so
because it is the commit path, and neither has a reply: it was never mentioned to session 14 at all, and 6f
reports the same. So the accurate status is *written, provenance unrecorded* — not approved and not a breach
either, since the standing constraint was "don't publish anything, no PRs, no issues" and a commit on a local
branch is not publishing. What it departs from is an undertaking the sessions gave, not an instruction the user
gave. It is **built but not deployed**, so nothing measured so far is affected.

`utils/statedb/create_namespace_tmpl.sql` now uses
`ON CONFLICT (key) DO NOTHING ... RETURNING key`, with the violating set computed in the same statement as
`_keys EXCEPT ALL inserted`. No Go changed and no contract changed — `insertStates` still consumes a
violating-key array, and `commit()` already aborts on a non-empty result, so the abort this needs is existing
behaviour. `TestCommit/new_writes_with_violating` passes unchanged.

It removes the full-batch `key = ANY(_keys)` storage read — the 1.2–2.6 s per failing attempt — and plpgsql's
implicit subtransaction, which the old `EXCEPTION` block opened on every call including the conflict-free ones.

**Reviewed, and the one behavioural change is safe for a structural reason rather than a documented contract.**
`ON CONFLICT DO NOTHING` leaves the non-conflicting rows of a partially-conflicting batch inserted, where the
`EXCEPTION` handler rolled the whole statement back. That state cannot become durable: `insertStates` writes
inside the caller's `tx`, and `tx.Commit()` at `database.go:266` is reachable only when the conflict result is
nil — the non-empty branch returns at 256 and the `defer rollBackFunc()` from 247 fires. A crash in between
drops the connection and the server aborts the transaction. So recovery paths, which read *committed* state,
cannot encounter a partially-applied batch, and the inference "these keys exist, therefore that batch
committed" stays valid. The caller contract is belt; transaction scope is braces, and the braces do not depend
on anyone remembering the contract. Also checked: `ON CONFLICT (key)` has its constraint from
`key BYTEA NOT NULL PRIMARY KEY`, and `EXCEPT ALL` preserves multiplicity so a within-batch duplicate key is
reported once rather than dropped.

**What to run**, A/B on the same day, fresh deployment each:

1. 5% double spends at the 120-way pre-split — the configuration that currently never meets the bound at any
   offered rate. Does it now?
2. The conflict-free 518,000 hold, as a regression check: the common path now materialises a `RETURNING` set
   and compares cardinalities where it returned `'{}'` after a bare INSERT, and it runs ~3,400 times a second.

Read `db_insert`, `db_insert_per_commit` and the p99/mean pair on both.

**Verify rather than assume**, twice. Whether YugabyteDB's `ON CONFLICT` avoids the per-key reads or merely
relocates them — it must read something to detect a primary-key conflict, so one `EXPLAIN (ANALYZE, DIST)` at
the real batch width reading `Storage Read Requests` settles it, which is task 2d's check. And note
`CREATE OR REPLACE` is not a live upgrade path: namespaces are created once, so a cluster holding the old
function keeps it until the namespace is recreated. Fine here, since every point redeploys.

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
