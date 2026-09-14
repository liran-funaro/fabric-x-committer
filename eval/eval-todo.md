<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Evaluation work items

Status 2026-09-14 13:00. **A conflicting workload has a sub-second operating point, and the conflict share
barely matters.** With pre-splitting off, both 5% and 10% double spends sustain ~90,000 tps at ~190 ms over
300 s. One arm can be up at a time and switching arms is a full bring-up (~10 min), so the two tables are the
two batches. `RUNNING.md` has how to run them.

## First positive result: pre-splitting off meets the bound

Two complete ladders, four 300-second holds each, every rung met. Verified from each row's own `vars` —
`reference_gap 300000`, `lookback 1000000`, `pre_split_tablets 0`, shape 2/0, `fast_block_prepare True`,
`inflight_growth 0`, `finished` equal to `offered`, abort matching the configured share:

| double spends | top committed | p99 | busiest CPU |
|---|---|---|---|
| 5% | **95,122** | 190 ms | 11% |
| 10% | **90,491** | 187 ms | 11% |

Both ladders **ran out of rungs while passing**, so these are lower bounds, not ceilings. Against the 120-way
split's ~20,300 — which never meets the bound at any offered rate — that is 4.7x on throughput *and* the
difference between meeting the bound and not. And a 5% shortfall for twice the double-spend rate, against a
25x collapse at the 120-way split, so **the share is not what costs**: a near-flat series across shares is
also the shape the published 9c panel has, which nothing measured here had reproduced before. ds20 and ds30
are running.

**Caveat with an expiry date.** `pre_split_tablets: 0` creates **one** tablet; splitting raises the running
count to **12** within five minutes and then stops, because the next step needs 10 GiB per tablet (~120 GiB of
table). So these ladders measured a stable 12-tablet layout — but a no-split table has the same destination as
one created with 120 (288 on twelve servers, which §6 records after eleven hours), it just starts ~120 GiB
away. Whether the 14 ms insert survives that step is **unmeasured**; `9c-nosplit-ds5-soak` is queued to force
it. Measured growth is 0.0146 GB per tablet per million transactions — ~685M transactions, ~180 GB of ledger —
so the crossing needs ~31 minutes at 350,000 tps — a rate never yet attempted here, so the soak may need extending.

**Why, in one controlled comparison.** Rung 1 against the 96-tablet row is matched on offered rate to within
4%, on retired rate to within 4%, and on conflict share and gap exactly — one variable:

| | offered | busiest CPU | µs/tx | insert | outcome |
|---|---|---|---|---|---|
| pre-split off | 15,000 | 1.9% | **79** | **15.2 ms** | meets, 408 ms p99 |
| 96 tablets | 15,659 | 33.8% | 1,382 | 1,764 ms | misses, 6.9 s mean |
| conflict-free, 120-way | 518,399 | 79.9% | 99 | — | meets |

**17.5x on CPU per transaction and 116x on the insert, at matched load** — so the utilization objection to
every earlier cost comparison does not apply here: load is held equal by construction. And at 79 µs against
the conflict-free 99 µs, conflicts add no measurable CPU per transaction once pre-splitting is off. (Both
µs/tx figures are busiest-host CPU times 64 threads over transactions finished, which attributes a whole
machine to one workload; and the 99 µs row is at capacity while the 79 µs row is at 1.9% CPU, so the right
claim is "no measurable addition", not that 79 is below 99.)

**The retry frequency is unchanged**: 1.911 attempts per commit against 1.91 at the 120-way split, on batches
**2.06x wider**. So the failure path is entered just as often on more keys and costs two orders of magnitude
less. That refutes per-key cost outright — 239x apart, with the width moving the wrong way.

**And the layout is 23 tablets**, read from the yb-master API (`/api/v1/tables`, then
`/api/v1/table?id=<uuid>`, count `tablets`). 23 → 88 is a 3.8x layout change for a 79x cost change, so
per-tablet is refuted too, by twenty-one. What is left is a **cliff between 23 and 88 tablets** that nothing
has probed — `tabhold32` and `tabhold48` are the queue's two most valuable remaining points, worth more than
`tabhold{88,96,120}`, which only re-measure the flat slow side.

## Watch this

The queue is `/data1/logs/fx-plan-14q-lf.sh`, running unattended in this order. Everything below the queue
is detail; this is the whole of what is outstanding.

| | batch | what it decides | state |
|---|---|---|---|
| 1 | `nosplit` | **Figure 1c.** Whether a conflicting workload has any sub-second operating point. Ascending fixed-rate ladders with pre-splitting off, at the documented reference gap. | **rungs 1–2 met**; 60k/100k to go |
| 2 | `ladder8tab`, `hold8` | The 8-tablet anomaly: one probe at 172,260 / 252 ms against six holds in the 20–30 s band. | queued |
| 3 | `ladderlow` | **Decides the section's conclusion, and is the only thing that can fill figure 1c.** The panel draws a collapsed bar only where a point *delivered* its offered rate and still missed the bound; every 120-way conflict probe was 9-24x capacity, so none qualifies and the panel prints "no rate qualified" as text instead. `ladderlow`'s sub-capacity rungs are the first rows that could be drawn. **Decides the conclusion:** Every gap-300,000 conflict row at the 120-way split was offered 25,000 tps or more, above its ~20,300 capacity — so it has never been given a sustainable rate, and "no rate qualifies however low" was never measured. Misses at 15,000 → the strong claim is earned; passes → pre-splitting costs capacity, not the bound. | queued, promote |
| 4 | `tabhold` | The tablet axis at one fixed rate, which is the only way it can be asked (see 2a). | queued |
| 5 | `ds1`…`ds0001` | Conflict share from 1% down to 0.001%, at fixed layout. | queued |
| 6 | `vc9` | The published tier width: nine validator--committers on the nine non-master database nodes. | queued |
| 7 | size sweep | **Figure 5 and Table 1** — 300 B re-measured, 3 KiB added, holds for 1 KiB and 4 KiB. The only end-to-end work left, and it needs its own arm. | queued last |

**Document state.** The double-spend section is written and its numbers are current. Figure 1c draws one
measured bar (0%) and four collapsed shares, which is what the measurement supports; batch 1 may add more.
Figure 5 and Table 1 wait on batch 7. One thing needs a decision rather than a run: **whether the generator's
deeper block buffer joins the tuned setup**, and so whether figure 2 carries that ladder as its own series
(item 8).

**`nosplit` ds5 is done and it is the section's positive result.** Four rungs, each held 300 s, each arriving
in full at flat in-flight, 4.88% abort throughout:

| offered | committed | p99 | insert | busiest CPU |
|---|---|---|---|---|
| 15,000 | 14,354 | 408 ms | 15.2 ms | 1.9% |
| 30,000 | 28,537 | 192 ms | 13.9 ms | 3.3% |
| 60,000 | 56,899 | 189 ms | 13.7 ms | 7.3% |
| 100,000 | **95,123** | **190 ms** | 13.6 ms | 10.6% |

So **at least 95,123 tps** — the ladder ran out of rungs, not the pipeline out of capacity — against ~20,300
at the 120-way split where no rate meets the bound. 4.7x and the SLO. The 408 ms is rung 1's warm-up; steady
state is 190 ms.

**The tablet count is a function of time, so no row may be labelled with one.** Three reads of a no-pre-split
`ns_0`: **2** three minutes after bring-up, **12** twenty-six minutes into the ds5 ladder, **23** later and
holding. All correct — YugabyteDB splits as the table grows, and 12 is the documented low-phase boundary (one
per server) it crossed rather than a resting point. `ns_0` created with 120 held 288 after eleven hours. The
document's column is now headed **pre-split** and the row reads **none**. Two consequences: `tabhold`'s rows
will drift during their own measurements unless splitting is pinned, and since insert cost *fell* while the
count grew sixfold during the ds5 ladder, the cliff sits between roughly **23 and 88** — so `tabhold32` and
`tabhold48` are the informative rungs.

**Also settled**: `yb-admin list_tablets` truncates at `max_tablets = 10` and exits 0, so any count of exactly
10 from it is suspect. Pass `0`.

**Audited and clean, so not worth re-checking**: every quantitative claim in the abstract against
`figures-orderer.jsonl` — 499,091 at 447/658 ms is a 300 s window fully retired with zero aborts; every rate
below it arrived within 0.2%; 8 shards 500,000 against 4 shards 499,091; four shards hold the lower p99 at
both matched rates; the busiest machine is `commit6`/`commit5` at the top and a router below; 81.6% is the
true maximum behind "no machine exceeds 82%"; and the 250,000 small-batch ceiling reads `blk_rate` **499.98**,
so "a block-rate limit near 500 blocks a second" is measured rather than inferred. Also audited: no plotted
point in either arm mixes configurations within an x, after the reference-gap filter.

**Nothing is publishable from a rate search after a miss.** Every descent probe inherits the previous rung's
backlog, so its latency is not its own — `tab96` returned "no rate met" from seven probes for that reason
alone. This is why the queue is ladders and holds rather than searches.

## Solved: why double spends collapse to ~20,000 tps

A back-reference puts an existing key in a batch's new-writes. `insert_ns` inserts the batch blind and
returns on success — **no lookup at all** — but on `unique_violation` its handler runs
`key = ANY(_keys)` over *every* key in the batch. At ~1,000 keys against 120 tablets that is 120,000,
past YugabyteDB's ~32,768 batching threshold, so it degrades to one storage read per key. The batch then
rolls back and the Go retry loop re-runs it.

| evidence | conflict-free | 5% conflicts, 120 tablets | 5% conflicts, 8 tablets |
|---|---|---|---|
| throughput | 518,727 tps | 21,273 | **172,260**, probe only |
| busiest host CPU | 80% | 71% | 21% |
| CPU per transaction | 99 µs | **2,133 µs** (21×) | 75 µs |

Plus, from the VC's own counters: insert latency **1.72 s per call at 1.91 calls per commit** — the retry
loop, first attempt violating and second succeeding.

**And the latency bound fails on service time alone, which needs no queueing argument.** One tab96 probe
ran at `inflight_growth` exactly 0 — offered 15,659, finished 15,636, 77% of capacity, 34% CPU — so its
numbers are a service time and not a backlog: insert 1.764 s x 1.90 attempts = **3.35 s inside the commit**,
against a 6.94 s mean. That splits the mean into 3.35 s of service and 3.59 s of queue. 120 tablets agrees
at 1.709 x 1.91 = **3.26 s**. So the 1 s bound is missed by more than 3x before any transaction waits for
anything, at both tablet counts — a measurement, not a derivation, and it is why no rate search at these
splits can find a passing rate however low it goes.

At 8 tablets a conflicting workload shows **no visible CPU penalty** — 75 µs against 99 conflict-free is
the same order. It is not evidence that conflicts are *cheaper*: that point committed 172,260 against
518,727, a third of the rate, so it also carries less queueing per transaction. The defensible claim is
that the 21× penalty is gone, not that the sign reverses.

**The 8-tablet column is one 90-second probe that no hold has reproduced, so nothing in it is quotable yet.**
`9c-ds5-split8` met at 172,260 and 252 ms p99, and the three 300 s holds that followed — offered
181,031 / 167,621 / 155,204, so at and *below* the rate that passed — all returned 122,798–130,233 committed
at a **29,900 ms** p99 with 25-26 s means. Those are real to bucket resolution, not clamps (see the p99 note
below), so the holds are a **measured failure** and the 8-tablet recovery is refuted at sustained rates rather
than merely unconfirmed. What is left unexplained is the probe: 172,260 at 252 ms, with a redeploy before the
first hold and that hold draining (`grow=-300/s`) while reading 29,900 ms. `hold8` still runs for that.

**RETRACTED: the no-pre-split series is a different workload, not a different split.** `split0-ds10/20/30`
were run with `loadgen_tx_reference_gap: 0` and a 10,000,000 lookback, against `300000` and `1,000,000` for
every `9c-ds*` run. At gap 0 a back-reference points at a key generated immediately before it, so the referent
is still in flight and the dependency graph serialises the pair instead of letting the conflict reach
`insert_ns` as an existence violation. Those runs measure graph serialisation, not the insert failure path,
and their 92,958 / 76,353 tps cannot be compared with the 120-way split's ~20,300. `fx-plot-figures.py:351`
had already removed the series from panel 9c for exactly this reason ("every one of them was measured with the
reference gap that made the workload a dependency convoy rather than a double spend") — I reintroduced the
error by matching rows on rate instead of on configuration. **Check `loadgen_tx_reference_gap` before
comparing any two conflict runs.** Note the gap is not uniform even within `9c-ds*`: `9c-ds5` and `9c-ds10`
used 1,000 while `9c-ds20`, `9c-ds30` and every `tab*`/`split8` run used 300,000.

**The layout result at one fixed workload**, which is what the document now carries. All rows 5% double
spends, gap 300,000, so the only variable is the pre-split:

| tablets | committed | meets 1 s? |
|---|---|---|
| 8 | 172,260 | yes, 239 ms — but a 90 s probe; its hold gave ~125,000 at >29.9 s |
| 88 | 27,400 | no |
| 96 | 20,800 | no |
| 120 | 20,300 | no |

A factor of 8.4 end to end, 6 if the eight-tablet hold is used instead of its probe, and only the smallest
split meets the bound at any offered rate. The three larger rows are retirement rates under saturation, which
survive queue depth. **Still open**: a confirmed 300 s hold at 8 tablets, which is the one thing figure 1c
needs — `hold8` in the chain.

The conflict-free side of the trade wants checking too: the "2.9x" in the document divides 518,000 by ~181,000,
and that ~181,000 is `split0-ds0`, whose three holds all failed (155,273–202,000 at 19–44 s means). Its one
met probe was 213,091 at 0.15 s, which would make the ratio 2.4x. It is also `tablets=0` rather than 8, so it
is not the same layout as the table above.

**p99 clamps at 60,000 ms only — below that it is bucket resolution, not censoring.** I first called every
repeated value a clamp; the `le` set is 2 ms–10 s, then 15, 20, 30, 45, 60 s, so 29,900 = 20,000 + 10,000 x
0.99 is what `histogram_quantile` interpolates when every observation lands in the 20–30 s bucket. Coarse but
real. Only 60,000 is a clamp (quantile in `+Inf`), proven by a rung reporting p99 60,000 ms with a **mean of
69,334 ms**. This inverts the 8-tablet reading: all six holds at 29,900 with 25–26 s means are a **measured
failure**, so 8 tablets is refuted rather than unconfirmed, and its 252 ms probe is the anomaly. Use the mean
in overload anyway; keep p99 for the region near the bound.

## The conflict result, measured 2026-09-14

**A conflicting workload does meet the one-second bound, and the layout is what decides it.** With
`pre_split_tablets: 0`, a 5% double-spend workload at gap 300,000 met the bound at **every rate offered**,
in four consecutive 300 s holds:

| offered | committed | p99 | mean | `db_insert` | attempts | keys | busiest CPU |
|---|---|---|---|---|---|---|---|
| 15,000 | 14,354 | 407 ms | 147 ms | 15.2 ms | 1.911 | 356 | 2% |
| 30,000 | 28,536 | 192 ms | 137 ms | 13.9 ms | 1.904 | 357 | 3% |
| 60,000 | 56,899 | 188 ms | 135 ms | 13.7 ms | 1.907 | 356 | 7% |
| **100,000** | **95,122** | **190 ms** | 137 ms | 13.6 ms | 1.907 | 356 | 11% |

Arrival was exact at every rung, queue growth zero, aborts 4.88% throughout. **The ceiling was never
reached** — 11% CPU at 95,122 tps — so this is a lower bound, and `9c-nosplit-ds5-hi` brackets it at
150k/250k/350k with the layout pinned. The 120-way split retires 20,300 and misses the bound by sixty
seconds, so no pre-split is better on *both* axes at every rate measured, not a trade. Rung 1's 407 ms is a
cold-start transient: its p99 is 2.8x its mean while every later rung is ~1.4x, and `db_insert` *falls*
from 15.2 to 13.6 ms as the rate rises 6.7x.

| | 120-way pre-split | no pre-split (settles at 23 tablets) |
|---|---|---|
| committed | 20,300 tps | **95,122** and still climbing |
| p99 | 60,000 ms (the histogram's top bucket) | **192 ms** |
| `db_insert` | 1,709 ms | **13.9 ms** |
| attempts per commit | 1.91 | 1.904 |
| width | 175 tx | 178 tx |
| busiest CPU | 71% | 3% |

**Attempts and width are identical to three digits while the insert falls 123x.** So the failure path is
entered exactly as often and costs two orders of magnitude less: the pre-split controls the *fan-out of the
failing lookup*, not how often it happens. That is the mechanism claim, and it no longer needs a cost law.

**"No pre-split" is not one tablet — it is 23, reached by automatic splitting and then stable.** Omitting
`SPLIT INTO` leaves YugabyteDB to choose, and it chose 5; automatic splitting (on by default, confirmed by
reading `enable_automatic_tablet_splitting` from the running master) then took it 5 -> 15 -> 19 -> 23 in
four minutes and stopped there. The stop is the phase boundary: splitting thresholds are set by tablets per
NODE, and with twelve tablet servers the low phase covers up to twelve tablets at a **128 MiB** threshold
while the high phase covers up to 288 at **10 GiB**. At 23 tablets the table is in the high phase with
169 MB per tablet, so it stays. `eval/scripts/fx-tablet-count.sh` records this per run.

Two consequences:

1. **The winning configuration is ~23 tablets**, between the 8 that gave a fast probe and the 88 that did
   not. The monotone reading holds: fewer tablets, cheaper fan-out.
2. **It is the parsimonious explanation of the eight-tablet anomaly** — 172,260 tps at 239 ms on a 90 s
   probe against six 300 s holds at ~30 s. Eight tablets is 0.67 per node, inside the low phase, so a hold
   is long enough to split while a probe is not. `9c-ds5-hold8-nosplitting` is the controlled A/B: the same
   six rates as `9c-ds5-ladder8tab`, differing only in `--enable_automatic_tablet_splitting=false`.

**And the strong negative claim is not yet earned.** Of 70 gap-300,000 conflict rows at the 120-way split,
the lowest rate ever offered is **25,000 tps** against its own ~20,300 capacity, and every one reports a
censored p99. So "no rate meets the bound however low" was an extrapolation from rows that were all past
capacity. `9c-ds5-ladderlow` (2,500 to 20,000) is promoted to the front of the queue for that reason: a
miss at 15,000 from a clean start earns the claim, and a pass narrows it to capacity alone.

## Committer arm (`inventory/cluster.yaml`)

| # | Experiment | ids | Status |
|---|---|---|---|
| 1 | ~~Every 500-transaction-block measurement, again, with the fast block producer.~~ **Done, null result**: 528,545 tps at 10,000-transaction blocks against 531,455 before, 380,673 at 500 against 379,764. Preparation was not the ceiling. It also answers 1b: the large-block ladder did not move, so figure 1 was never generator-bound. The buffer was: 430,145 tps with a 2,000-block buffer. Original text below.<br><br>**Every 500-transaction-block measurement, again, with the fast block producer.** The old ladder measured the generator: its mock orderer prepared blocks on one goroutine at 0.75 ms each, capping it near 850 blocks a second. `fast-block-prepare` is now in the branch, the collection and the staged binary. Benchmarked here: preparation is a *per-transaction* cost, so it capped a transaction rate rather than a block rate — 775,500 tps at 500 a block and 777,500 at 10,000, on one goroutine of this workstation. The fix is 60x at 500 and 1,133x at 10,000. So **both** ladders need re-running, and if the cluster's generator prepares slower than this machine (2.10 GHz there), both were capped by it. | `curve`, `curve500`, `curve500hi`, `curve500top` | **ready, do first** |
| 1b | **Do figures 1a and 1b need re-measuring too?** Their points were taken with the old block producer at 10,000-transaction blocks, where preparation cost 12.1 ms a block — 63% of one goroutine at 518,000 tps and **79% at 653,273**. Close enough to a single-goroutine ceiling to be suspect. #1's 10,000-block ladder answers it: if it now sustains more than 531,455 tps, the whole of figure 1 was generator-bound and needs re-running. | `9a-*`, `9b-*` | **no — answered by 1** |
| 2 | ~~Why double spends collapse.~~ **SOLVED — see the section below.** It is `insert_ns`'s exception handler: one conflicting key makes the whole batch look up every key it holds, which past YugabyteDB's batching threshold costs 1.72 s and a rollback. | `9c-ds5-*` | done |
| 2a | ~~Tablet sweep at 5% conflicts.~~ **Closed — no cost law is identifiable from it, and the document does not need one.** Measured: 88 tablets retires 27,400 tps, 96 retires ~20,800, 120 retires 20,400, 8 tablets recovers to >170,000. Retirement rates are sound (a saturated pipeline retires at capacity whatever the queue depth). The per-batch *costs* are not: every probe moves tablet count, batch width and utilization together, so three cost laws were each fitted and refuted by the next batch — the 32,768 product, per-tablet, per-key×tablet. `tab96` shows it without any cross-tablet comparison: at fixed tablets and a width held at 601–611 keys, the insert still moves 2.316 → 2.033 → 1.850 s as the offered rate falls. Three retractions on the same question is a signal about the instrument, not the mechanism. | `9c-ds5-tab*` | closed |
| 2a-i | **What is settled, and holds under every reading.** The failure attempt costs **1.2–2.6 s** against the 19–23 ms `tx_batch_commit` span that was being sampled — a range, not a number, because the conflict histogram is in the code but *not* on the deployed binary, so the single histogram blends one clean attempt with the 0.79 failing ones per commit. The same INSERT to the same ~120 tablets is cheap when nothing violates (518,000 tps), so the cost is the failure path, not write fan-out. The **32,768 threshold is refuted here**: 301 keys × 88 tablets = 26,488 is under it and the insert is still 1.2 s, and the 88→120 step is 42%, not a factor of 24 — §6's constant stands for the read path it was fitted to. Also settled: **table size is not a variable** (8.8× growth, 637k→5.6M rows, at fixed width moves the insert +0.8%), and `committed/insert_count` is the width *divided by attempts*, not the width — real width is 150.5 tx = 301 keys at 88. | — | done |
| 2a-0 | **RETRACTED: there is no established cost law, and the tablet axis as measured cannot produce one.** Three laws were fitted and all three fail once the rows are separated by overload depth — per-tablet 80%/57%, per-key 46%/31%, per-key-x-tablet 31%/43%, against 8.8% repeatability. The confound: **`db_insert` under conflicts is not a service time.** At 96 tablets it reads 2.32 s, 2.03 s and 1.85 s at offered 25,500, 21,675 and 18,423 with the width held at 601–611 keys, and 1.33 s while draining — same deployment, same tablets, same width, a 75% range. The batch width moves with backlog depth too, so two probes at unmatched distances above capacity differ in queueing as much as in tablets. The "13.8 ms per tablet, confirmed at 1.7%" recorded here earlier was three rows that happened to share a 300–350 key width, **one of them a drain window** — it is withdrawn, and per-tablet is untested rather than refuted.<br>**Gating rows on `\|inflight_growth\|` does not fix it**: a queue pinned at its ceiling has zero derivative, so the tab88 rows at 8.6x capacity read −1,111/s and pass any growth filter while tab96's cleanest row fails one. The usable gate is **offered-against-retired together with mean latency** — arrival plus a tail that isn't a backlog — both already in the row schema.<br>What replaces it: `9c-ds5-tabhold*`, a fixed 10,000 and 15,000 tps at 32/48/64/88/96/120 tablets. Both rates clear every capacity in the sweep, so the backlog stays near zero and tablets are the only difference; the second rung exists because one fixed rate cannot show that it is uncontaminated and two can. **None of the document's claims depend on the law**, so this is not a blocker for anything. | `9c-ds5-tabhold*` | queued |
| 2a-iii | **What `db_insert` actually spans, from the source.** It wraps `insertStates` only (`service/vc/database.go:381`), so it covers the blind `INSERT`, the **plpgsql savepoint rollback** (`insert_ns_*` has an `EXCEPTION WHEN unique_violation` block, so the failed statement is undone before the handler runs) and the handler's `key = ANY(_keys)`. It does **not** cover the outer distributed transaction's rollback — `commit()` returns early on `res != nil` and its deferred `rollBackFunc` fires *after* `insertStates` returns — nor the retry loop in `commitTransactions`. **So the cost per commit is higher than `attempts x db_insert`, not lower**, and this is also why `tx_per_insert` reports width/attempts: one observation per attempt.<br>The conflict-free case is the control that puts the cost on the failure path — the same `INSERT` to the same ~120 tablets is cheap when nothing violates (518,000 tps) — which leaves the savepoint rollback and the handler's lookup, and no measurement yet separates those two. `hold8` is the one that can: at 8 tablets there are ~38 keys per tablet against ~2.5 at 120, so a flat per-tablet cost predicts 0.11 s while a cost that grows with keys-per-tablet predicts more. | — | analysis done, `hold8` decides |
| 2a-ii | **If the cost law is ever wanted: one fixed offered rate at every tablet count.** 15,000 tps clears every capacity in the sweep (20,400 at 120, ~20,800 at 96, 27,400 at 88), so the backlog stays near zero and the tablet count is the only difference. 300 s per point, fresh deployment each, width and insert read in-window. Cheap — no rate search, so no seed-floor trap — but **below the e2e sweep in value**, and nothing in the document depends on it. | new | optional |
| 2b | **Conflict-share sweep**: 1%, 0.1%, 0.01%. P(a ~500-transaction batch holds a conflict) is 94%, 25%, 2.8%, so throughput should climb steeply below 1%. Tests "per batch, not per conflict" with no code change. Note the measured attempts per commit is **1.79**, not the >=2 a binomial spread at width 150 and 5% would give, so conflicts are clustered by the dependency graph and the binomial arithmetic above is an upper bound on the affected share. | new | ready |
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
| 8 | **Resolved, except one part that is genuinely yours.** The section no longer attributes the small-block ceiling to block preparation, which the re-measurement refuted (528,545 and 380,673 tps with preparation 1,133x cheaper). It says what was measured: the bound is the generator's outstanding-work buffer, counted in *blocks*, so deepening it twentyfold moves the ceiling from ~400,000 to **430,145 tps at a 209 ms median** (p99 408 ms, 300 s window, generator at 21%) — verified against the row.<br><br>**Decided: figure 2 does not carry the deeper-buffer ladder as a second series.** The buffer is a *load generator* knob, so deepening it removes a measurement artefact rather than tuning the system under test; and splicing two generator configurations onto one axis is the error class corrected six times today. `curve500buf` already has its own figure id, so nothing is contaminated either way. The text carries the number, which is the honest treatment.<br><br>**Yours to decide, because it changes what future runs measure**: whether the deeper buffer becomes the standard generator configuration. If it does, the whole curve should be re-measured at it rather than spliced — one configuration per figure. | decided / one part yours |
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
