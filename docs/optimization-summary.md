<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->

# Optimization summary and issue plan

Everything on this branch that changes throughput, latency or allocation behaviour, relative to
`main`, together with how it is to be tracked. `cluster-optimization-log.md` is the narrative and
carries the evidence for every number here; this file is the index and the issue plan.

Two scoping rules apply throughout:

- **Section 4 is not filed and not proposed as repo defaults.** It is deployment tuning for this
  evaluation's nineteen-machine cluster, recorded here so the numbers in the other sections can be
  reproduced. Values that belong in a shipped default would have to be argued separately, on
  hardware that is not this cluster.
- Commit hashes are given for orientation only. This branch is rebased onto `main` regularly, so
  match on the subject line rather than the hash.

## How this maps to issues

| | Tracked as |
|---|---|
| Umbrella | one new issue, referencing every child below |
| Sections 1, 3, 5 | one child issue each row |
| Section 2 | an issue in the `fabric-x-common` repository, referenced by the umbrella here |
| Section 4 | not filed — deployment tuning for this cluster |
| Section 6 | not filed — the constraint is outside the committer, and 6.1–6.3 are results rather than work |

`#772` already covers row 1.2 and needs no new issue.

## 1. Committer code optimizations

| # | Optimization | Measured | Commit |
|---|---|---|---|
| 1.1 | **[sidecar] Make the block store transaction ID index optional** — the index wrote one LevelDB entry per transaction rather than per block, and its compaction grew with the ledger | 115,200 → 297,200 tps; removed 35% of sidecar CPU and a decay from 102,102 to 67,886 tps over two hours | `5bba0aa8` |
| 1.2 | **[sidecar] Replace the relay's sync maps with single-owner tracking** — two `sync.Map`s touched four times per transaction, at 100% key churn | 297,200 → 305,600 tps | `495aeea6` — **#772** |
| 1.3 | **[coordinator] Allow selecting the simple dependency graph manager** | +47.6%, 329,854 → 486,941 tps; the default manager starved the database at 62 ms batch commit and 60% CPU | `9bdabd00` |
| 1.4 | **[coordinator] Stop the simple dependency graph latching the pipeline** — prerequisite correctness fix, not a gain in itself | without it the pipeline halts under load | `c9dd45c8` |
| 1.5 | **[sidecar] Parse a block's transactions in parallel** — up to 16 goroutines, with the fold-in and the dedup still ordered | mapping 5.3×, 408,000 → 2,308,000 tx/s; worth +26% end to end (497,000 → 628,000) but *nothing* until the block store stopped being the binding stage | `069c1d29` |
| 1.6 | **[sidecar] Stop allocating per transaction in key validation and TX references** — a map and a slice per namespace in `verifyTxForm`, plus `TxRef`/`TxWithRef` | 61 → 56 allocs/tx, +12% at default `GOGC` | `128bb575` |
| 1.7 | **[sidecar] Back a block's decoded TXs with one allocation** — `UnmarshalTxInto` writes into a per-block slab | 19 → 18 allocs/tx, identically at every block size | `02e9b9e8` |
| 1.8 | **[sidecar] Separate mapping's scaffolding from its result** — the result no longer carries the slabs, the dedup set or the collected TX IDs | releases one string slice per in-flight block; allocations unchanged | `0243503f` |

The largest committer code optimization of all is **section 5**, kept separate because the account of
how it was found is most of its value. Treat it as row 1.9 when filing.

## 2. fabric-x-common

| # | Optimization | Measured |
|---|---|---|
| 2.1 | **[blkstorage] Do not build tx index information no index will read** — `serializeBlock` extracted a txID for every envelope, and built a `txindexInfo` and a `locPointer` each, whatever the store was configured to index | ledger append 22.2 → 7.1 ms per 10,000-transaction block; the stage was at a 100% duty cycle and capped the pipeline at ~451,000 tps |

Filed in `hyperledger/fabric-x-common`. The committer's own issue for it is the umbrella's reference,
because the committer is where the effect is measured: with `disable-tx-id-index` set, a sidecar pays
for index information nothing reads.

## 3. Benchmarks and measurement apparatus

Part of the optimization work rather than a by-product: three of the findings above were invisible
until the corresponding benchmark existed.

| # | Change | Why it mattered | Commit |
|---|---|---|---|
| 3.1 | **[sidecar, test] Add an end-to-end sidecar benchmark** — whole service on one machine, real ledger, real gRPC, stubbed orderer and coordinator | the only way to attribute a sidecar stage without a cluster; found the block store ceiling | `a90b7db6` |
| 3.2 | **[loadgen] Benchmark the submit path, not just generation** | separated the generator's ceiling from the committer's, which a ramp cannot do | `2e575dd3` |
| 3.3 | **[loadgen] Add generation sweeps for the rate a deployment can offer** | showed the generator's plateau moves with core count, so a setting must be measured on the machine that will run it | `9b3dbefb` |
| 3.4 | **[coordinator] Fix a benchmark that could never run the default manager** | the manager comparison was invalid before this | `5e9a7ef8` |

## 4. Configuration and deployment tuning — not filed

Recorded so the figures above are reproducible. Not proposed as repo defaults.

### 4.1 Committer configuration

| Setting | Change | Measured |
|---|---|---|
| Block size | 500 → 10,000 | two sidecar costs are per-block, not per-transaction; block signature verification alone goes 169,000 → 1,281,000 tx/s |
| `sidecar.channel-buffer-size` | 100 → 5 | latency 6,355 → 1,156 ms at unchanged throughput; the buffer held 237 submitted blocks whose statuses had not returned |
| `coordinator.dep-graph-wait-tx-limit` | 20,000,000 → 500,000 | 100× less coordinator memory (79 GB → 786 MB) at unchanged throughput |
| `sidecar.waiting-txs-limit` | must be **≥** the coordinator's window | below it, the sidecar's window silently replaces the coordinator's as the binding one and caps throughput — this produced a wrong 10% "cost of the sidecar" before it was understood |
| VC committer workers / pool | 32 → 64 per VC, pool 64 → 128 | +6.8%, and the run stopped decaying; occupancy was 190.9 of 192 while the machines sat at 74% CPU |

### 4.2 Load generator

Three of these are code changes on this branch rather than settings, but they belong with the
measurement apparatus rather than with the committer's own optimizations. They will still need a PR
even though no issue is filed for them.

| Change | Measured | Commit |
|---|---|---|
| Ed25519 instead of ECDSA | 325,600 → 374,400 tps — this removed the *generator's* ceiling, not the committer's. The win is allocation, not entropy: Ed25519 allocates 184 B and 4 objects per signature against ECDSA's 6,067 B and 59, and `GOGC=off` gives ECDSA 5.2x while barely moving Ed25519. Ed25519 reads no entropy at all — verified by a goroutine dump on the cluster's own load generator with zero `GetRandom` frames under load. See `cluster-optimization-log.md` section 5. | config |
| `gen-batch` 100 → 4,096 | 495,583 → 629,884 tx/s generated; 16,384 is faster still but costs four seconds of startup dead time | config |
| **[loadgen] Expose the target transaction rate as a metric** | distinguishes "the generator is the limit" from "the committer is" on every line of the harness | `e9e8c8eb` |
| **[loadgen] Let the sidecar adapter bound the mock orderer's block buffer** | without a bound, an overloaded committer is absorbed rather than felt: submitted rate stays at the requested rate while latency grows without limit | `69ef27dc` |
| **[loadgen] Fix the latency tracker's ignored max-tracked-txs setting** | an undersized table drops samples, biased towards slow transactions, so the reported mean was pulled down | `52ae5564` |

### 4.3 Database

| Setting | Decision | Measured |
|---|---|---|
| Front end across all twelve nodes (`load-balance` plus cluster-wide certificate SANs) | fixed | a real defect — every connection sat on one tablet server — but worth no throughput on its own |
| State table pre-split | **kept at 120 tablets**, ten per tablet server | see below |

**Pre-split is retained, deliberately.** It is worth **+35% on this evaluation's workload** — 486,941
tps against 359,866 at 12 tablets — and every headline figure in these documents was measured with it
on. Its cost is real but conditional: a multi-key lookup (`WHERE key = ANY($1)`) issues one storage
read request per key instead of one per tablet, so a blind-write workload commits 13,160 tps at 120
tablets against 314,336 at 12, a factor of 24. That cost is unobservable while every transaction only
inserts fresh keys, because nothing then performs a multi-key lookup.

What governs the cliff is not the tablet count but the **product of tablets and keys per lookup**,
which has to stay under roughly 32,768 on this hardware. So the durable lever is the committed batch
width, and the tablet count is not a lever at all: YugabyteDB splits as a table grows, and `ns_0`
went from 120 tablets to 288 over eleven hours of load. Lowering the initial count postpones the
cliff rather than removing it.

## 5. gRPC flow control was the constraint, and raising it is worth 13%

| # | Optimization | Measured |
|---|---|---|
| 5.1 | **Size HTTP/2 flow control for the rates in play** — `grpc.WithInitialWindowSize`/`WithInitialConnWindowSize` on clients and the matching server options, 16 MB per stream and 32 MB per connection | over-driven mean 510,371 → **578,383** (+13.3%), peak 528,800 → **590,400** (+11.6%); at a sustainable 500,000 the latency falls **645 ms → 392 ms**, a 39% reduction at the same rate |

Both over-driven figures are from runs taken first on a fresh deployment, which is the only way they
compare: the same configuration measured 544,260 when its over-driven run followed two five-minute
holds, because the state table had grown by ~300M rows in between. Section 6.4 of
`cluster-optimization-log.md` covers that decay.

Nothing in the repository set a window, so gRPC's defaults applied. A goroutine dump of a saturated
coordinator found **all three** senders to the signature verifiers and **five of six** senders to the
validator-committers blocked in `grpc/internal/transport.(*writeQuota).get` — out of stream send
quota. They were not slow: each used a quarter of a core. Meanwhile the verifiers held 1,700
transactions of a 128,000 capacity and ran 22 of 64 cores, 78% of that in real signature
verification. The senders were not allowed to write, so the verifiers starved.

After the change the same dump has **zero** senders blocked on write quota — one is instead idle
waiting on its input channel — and verifier in-flight more than doubled.

Two wrong hypotheses preceded this, both recorded because they are the obvious readings:

1. *"`batch-time-cutoff` of 2 ms cuts verifier batches before they fill."* Wrong: `BatchSizeCutoff`
   and `BatchTimeCutoff` govern only the **output** batching of statuses, and `Parallelism` counts
   per-transaction verify goroutines, not goroutines within a batch.
2. *"128 workers contend on two per-transaction channels inside the verifier."* Wrong twice over:
   those channels are sized 128,000 and held 1,700, and the verifier's single receiving goroutine is
   2.5% of its CPU. A benchmark added for this
   (`service/coordinator/signature_verifier_manager_bench_test.go`) measures the manager at
   ~560,000 tx/s on one stream and ~1.08M on three, against mock verifiers that do no signature
   work — 3.3× the cluster's per-sender rate, which is what ruled the manager out before any code
   was changed.

### Reading latency on an over-driven run

The over-driven run reported up to **39 s** mean latency, which is not the committer's latency. At
its peak the harness counted **22.8 million** transactions in flight while the committer's own
windows hold at most 1.5M (the sidecar's 500,000, the coordinator's 500,000, and a 1M mock-orderer
ring). The remaining ~20M sat inside the **load generator**, which reported 10 GB resident. In-flight
then *decayed* to 3.5M across the run at constant throughput, which is the signature of a startup
backlog draining rather than a steady state.

At a sustainable rate the same deployment measures 562 ms with 481,250 in flight — essentially just
the sidecar's window, which also confirms the 16 MB gRPC windows add no buffering at operating rates;
they are credit limits, not filled buffers.

Two of the holds above report 2 ms and 5 ms, which are artifacts, not results: Little's law put the
same rows at 3,105 ms and 5,514 ms. Whenever `little_ms` and `hist_ms` disagree by more than about
3×, trust `little_ms` — `inflight / throughput` needs no sampling and cannot lose a measurement.

The cause is **not** established, and the obvious suspect has been ruled out by measurement.

That suspect was the latency tracker (`loadgen/metrics/tracker.go`), which indexes sampled
transactions by `hash(txID) % max-tracked-txs` and *overwrites* on collision, so a sampled
transaction whose slot is taken before its status returns is never measured — and the loss is biased
towards slow ones. Driving the real tracker at the deployed sizing (100,000 slots, 1% sampling,
500,000 tps) and replaying arrivals in order gives:

| true latency | sampled transactions still measured |
|---|---|
| 300 ms | 98.4% |
| 920 ms | 95.4% |
| 3.1 s | 85.6% |

Losing 14% of samples cannot turn 3,105 ms into 2 ms. So the tracker is not the explanation, and
something else in the histogram path is. Until that is traced, do not quote a histogram-derived
latency when `little_ms` disagrees with it by more than about 3× — and note that all the latencies
quoted in this document as *results* come from rows where the two agree.

One structural observation while chasing it: **no measured latency in this evaluation comes from the
committer.** Every figure is the load generator's, from `OnSendBatch` to `OnReceiveBatch`, so it
carries the generator's own queueing and depends on the generator's sampling being sound. The sidecar
exposes per-stage block timings but nothing for a block's whole round trip — received to statuses
returned. A single sidecar-side histogram of that would give a latency signal that does not depend on
the harness at all, and would have settled today's 39-second question in one query instead of an
in-flight accounting exercise. Worth adding before the next latency claim.

What the arithmetic does establish is that these are not slow transactions being under-counted: no
transaction can *complete* in 2 ms when database commit alone is ~200 ms, so the observation itself is
impossible as a latency. `duration` is `time.Since(tx.created)`, so `created` must have been set about
2 ms before the status was handled.

Two candidate causes have been checked and eliminated:

- **The tracker's hash-with-replacement table** — measured above, 85.6% of samples survive at a 3.1 s
  latency.
- **The send path recording a transaction twice**, which would refresh `created`. It does not:
  `sendBlocks` calls `OnSendBatch` once per block after the send returns, `mock.Orderer.SubmitBlock`
  blocks on a channel rather than dropping or retrying, and each block's transaction IDs are unique.

So the cause is still open. It is a measurement defect, not a pipeline behaviour, and the practical
rule above is enough to work around it — but it should be traced before anyone quotes a
histogram-derived latency near these rates.

## 6. The committer is no longer the bottleneck — the database is

Located after 5.1 moved the constraint. Every committer stage now has headroom:

| Stage | Utilisation |
|---|---|
| sidecar | 14% CPU |
| coordinator | 21% CPU |
| verifiers | 34% CPU, 78% of it real signature verification |
| VC preparer | 4.0 of 96 workers concurrently busy |
| VC validator | 16.8 of 96 |
| **VC committer** | **383.9 of 384** — the only saturated stage |
| Every queue in the pipeline | zero, except the sidecar's own backpressure window |

But the commit stage is not concurrency-limited any more, it is waiting on the database. Raising its
workers from 32 to 64 per validator-committer moved concurrency 228 → 384, **+68%**, and bought
**+1.7%** of database batches per second — 1,431 to 1,455 — while commit latency rose 160 → 264 ms.
Adding concurrency to a saturated resource only queues.

What is saturated is the database, and **which of its resources binds migrates as the dataset grows**.
That is why two earlier revisions of this file contradicted each other; both were right about the
point they measured.

| | committed | db CPU | disk busy | disk writes | disk reads |
|---|---|---|---|---|---|
| fresh, through the sidecar | 515,826 | 82% | 72% | 3,751 MB/s | — |
| fresh, coordinator-direct | 597,498 | 81% | 52% | 3,985 MB/s | ~30 MB/s |
| steady after 2.4 h at 500,000 | 501,026 | 83% | **88%** | 3,772 MB/s | **1,753 MB/s** |
| decayed after 3.4 h, ~8 TB | 458,968 | 83% | **91%** | — | **2,453 MB/s** |
| during a transient stall | 451,654 | 90% | **100%** | 6,363 MB/s | **4,542 MB/s** |

While the tables are small the database is CPU-bound at about 81% and the disks have headroom — note
the second row has both higher throughput and *lower* disk utilisation, which is the wrong way round
for disk to be binding. As the dataset grows, compaction read traffic climbs from ~30 MB/s to
1,753 MB/s and disk utilisation with it, from ~52% to 88%, while CPU stays flat at ~83%. So the
constraint migrates from CPU to disk, and it arrives at about three hours at this rate. Note that CPU
never becomes the limit — it sits at 81–83% throughout, because the database cannot get more work
through its disks to use it.

The reads are the tell. They are **entirely compaction**: `vcservice_database_tx_batch_query_version`
records 0 lookups per second, because this workload only inserts fresh keys and never performs a
multi-key read. So the reads are the database rewriting its own SST files, competing with user writes
for the same devices.

It shows up first as an occasional **transient stall** — a compaction burst takes the disks to 100%,
throughput drops for a few minutes, then recovers — and later as sustained decay once compaction can
no longer keep up between bursts. Section 2's eleven-hour run is further along the same curve, at
15.4 TB and 85-99% disk, where it had fallen to 330,000. Per transaction the committer writes one `tx_status` row and two
state rows, and `insertTxStatus` already batches a whole batch into one array round trip
(`service/vc/database.go:320`), so there is no round-trip inefficiency left to remove. The commit
splits 121 ms inserting keys and 104 ms writing statuses.

Raising throughput further therefore means writing less per transaction — a schema and design
question about `tx_status` granularity — or more disk. It is not a committer tuning problem.

**Latency is database-bound too, by the same stage.** At a sustained 500,000 tps the pipeline holds
230,000 transactions, and Little's law puts that at 460 ms against 519 ms measured. It divides in two:

| Where | In flight | Latency |
|---|---|---|
| VC commit stage — 384 concurrent batches × 362 transactions | 139,000 | 278 ms |
| Sidecar's waiting window — submitted, awaiting status | 91,000 | 182 ms |

So 60% of end-to-end latency is work sitting in the commit stage. Reducing it means reducing that
stage's concurrency, which section 6.1 measured as costing 17% of throughput for no latency gain at
equal rate. Both throughput and latency therefore bottom out on the same resource.

**The sidecar is not the next constraint**, checked from the other side: at 51.6 blocks/s its worst
per-block stage is mapping at 11.21 ms of a 19.38 ms budget, 58% duty, with block send at 41%, ledger
append at 29% and signature verification at 16%. Even at the over-driven ceiling mapping is about 65%.
Worth noting that the flow-control fix also cut the block-send stage from ~13 ms to 7.87 ms, because
that stage contains the `stream.Send` that was blocking on write quota.

### 6.1 Lowering committer concurrency was tried and reverted

The reasoning was that since the database is saturated, the extra concurrency should only queue: it
had bought +1.7% of database batches per second for +65% of commit latency. Measured at each
configuration's own ceiling, that is wrong:

| workers per VC | concurrent commits | over-driven mean | peak | db commit |
|---|---|---|---|---|
| 40 | 240 | 493,508 | 503,200 | 171 ms |
| **64** | **384** | **578,383** | **590,400** | 264 ms |

64 is worth **17% more throughput**, and at equal offered rate the end-to-end latency is the same
either way — 501 ms at 510,000 with 40 workers against about 520 ms interpolated with 64. The lower
setting buys latency only by giving up throughput, so it was reverted.

The error is worth recording: the "+1.7%" came from comparing a 32-worker run taken *before* the gRPC
flow-control fix with a 64-worker run taken *after* it, both at the same offered rate. Two
configurations have to be compared at each one's own ceiling, not at a rate they can both serve.

### 6.2 Pipelining the commit's round trips would not help — issue #307

Each commit is four sequential round trips: `BEGIN`, the `tx_status` insert, one state insert per
namespace, `COMMIT`. Batching them with `pgx.Batch` is open as #307, and for this workload it is
worth roughly 0.4%.

The reason is that a batch on one connection still executes its statements **sequentially
server-side**, so pipelining removes network round trips and not database work. Of the 264 ms commit,
104 ms is the status insert and 121 ms the state insert, and both are execution and queueing inside
YugabyteDB; a local-network round trip is a fraction of a millisecond. The saving is one round trip
out of 264 ms.

It may still be worth doing for a latency-sensitive deployment with small batches, where the fixed
round-trip cost is a larger share. It is not a throughput lever here.

### 6.3 The one committer-side lever left, and why it is not safe

`tx_status`'s primary key is a transaction ID, and the committer stores it as the 64-character hex
string `protoutil.ComputeTxID` produces — `hex.EncodeToString(sha256.Sum(...))` — so 64 bytes carry
32 bytes of entropy (`service/vc/database.go:341`, `ids = append(ids, []byte(status.Ref.TxId))`).

Quantified: 32 wasted bytes per transaction at 527,000 tps is 16.9 MB/s of logical writes, and the key
lands in the row and in the LSM's index and bloom structures. At the ~21× amplification measured
above that is roughly 350 MB/s of the 3,751 MB/s the database writes — about **9%** of the resource
that now caps the pipeline. Decoding the hex on write and re-encoding on read would be contained to
the validator-committer, since it changes storage rather than any protocol.

It is nevertheless not a safe change as stated, for three reasons worth writing down so the next
person does not start it:

- **Transaction IDs are arbitrary strings, not necessarily hex.** Fabric lets a client choose its own.
  A decode would have to fall back for non-hex IDs, which reintroduces variable-length storage and
  the branch that goes with it.
- **The snapshot hash covers `tx_status`** (`service/vc/database_snapshot_hash.go`), so changing the
  stored bytes changes the digest and breaks comparison across versions.
- Existing deployments' rows would no longer be readable without a migration.

A narrower version — store the decoded form only when the ID is exactly 64 hex characters, with a
length-tagged fallback — is possible but is a schema change with a migration, not a tuning knob.

## Where the pipeline stands

| | Throughput | Note |
|---|---|---|
| Start of the evaluation | 80,000 tps | |
| After sections 1-4 | 500,000 tps requested and delivered, 99.9%, 645 ms mean | sustainable-rate hold, sidecar in the path |
| Same deployment, over-driven | 510,371 mean / 528,800 peak | asking 1,000,000 |
| **After section 5 (gRPC windows)** | **505,900 at 392 ms; 519,800 at 472 ms** | sustainable holds, fresh deployment, holds run first |
| Same, held for 30 minutes | **499,450 at 519 ms, 99.9% of a 500,000 request** | flat, no decay over ~900M transactions |
| Same, over-driven | **578,383 mean / 590,400 peak** | asking 1,000,000, run first on a fresh deployment |
| Coordinator-direct, before section 5 | 525,388 mean / 533,213 peak | 45 min, and it rose into that figure: its own first 25 min averaged 482,422 |
| **Coordinator-direct, with section 5** | **570,321 mean / 614,173 peak** | 43 min; its first 25 min averaged 581,158 |
| Same, held at a fixed 500,000 for 4 h | **500,000 for the first 3 h at ~315 ms**, then ~450,000 at ~1,300 ms | decay onset at ~7 billion transactions and ~8 TB, from compaction saturating the disks |

**The sidecar still costs essentially nothing**, re-checked at the higher rate on matched windows:
578,383 through the sidecar against 581,158 coordinator-direct over each run's first 25 minutes —
**0.5%**. And the flow-control fix is worth about as much in either topology, +13.3% through the
sidecar and +15.2% on the coordinator-direct peak, which is what you would expect of a change to the
coordinator's streams to the verifiers and validator-committers, since both topologies have them.

Comparing the two coordinator-direct runs needs care: the pre-section-5 one stepped *up* mid-run for
reasons never identified (section 6.4), while this one decays as state grows. Matched early windows or
peaks are the honest comparison, not the full-run means.

Latency at a given rate improved along with throughput, which is unusual enough to state plainly:
500,000 tps cost 645 ms before section 5 and 392 ms after. Removing the flow-control stall let the
pipeline stop holding work upstream, so in-flight at 500,000 fell to 230,000.

### 500,000 tps held for three hours, then decayed to 450,000

A fixed 500,000 tps request for four hours, coordinator-direct so the sidecar's append-only ledger
could not end the run first, sampled every five minutes and averaged into quarter-hours:

| Elapsed | Committed | Latency | |
|---|---|---|---|
| 0–120 min | 499,416–500,879 | 302–324 ms | flat |
| 120–135 min | 492,724 | 918 ms | transient stall |
| 135–150 min | 509,854 | 524 ms | above target, draining the backlog |
| 150–180 min | 499,445–499,799 | 329–368 ms | recovered exactly |
| 180–240 min | 430,527–477,444 | 1,219–1,377 ms | sustained decay |

**Three hours at 500,000 tps, then a settled ~450,000 at four times the latency.** Onset at roughly
7 billion transactions and 8 TB across the twelve machines.

The stall at 125 minutes is worth separating from the decay at 180, because they look identical in a
single sample and are not the same thing. The first recovered completely — two samples above target
while the backlog drained, then the original steady state to within 0.3%. The second did not: it has
persisted for twelve consecutive samples and the offered rate stays below the target throughout. A
five-minute sample cannot tell them apart, and an earlier revision of this file read the 125-minute
one as the onset of decay and had to be corrected.

### 500,000 tps held for half an hour, with the sidecar in the path

A fixed 500,000 tps request, sampled every 60 s, excluding the first three minutes while the previous
run's backlog drained:

| | |
|---|---|
| offered | 500,017 |
| committed | **499,450 — 99.9%** |
| mean latency | **519 ms** |
| five-minute buckets | 498,720 / 500,320 / 499,760 / 498,720 / 499,200 |

Flat: no decay across thirty minutes and roughly 900 million transactions. Latency drifts from
505 ms to 534 ms and database commit from 224 ms to 242 ms over the window, which is the state-growth
effect of section 2 appearing gently rather than the throughput cliff the eleven-hour run found at a
higher operating point.

The 392 ms figure above and the 519 ms here are both honest and differ only in how much state the
database already held: the first was the first measurement on a fresh deployment, the second came
after roughly 1.5 billion transactions of prior load in the same cycle.

**A soak with the sidecar in the path cannot run much longer than this.** The ledger is append-only
with no retention, so at 500,000 tps it writes ~167 MB/s and fills the sidecar's 969 GB disk in about
1.6 hours. That is why the eleven-hour decay curve in section 2 was measured coordinator-direct.

Every figure above depends on how full the state database was and on the day it was taken: this
cluster's baseline moves about 15% between days with identical code, which is recorded in
`cluster-optimization-log.md` section 6.4 along with what that cost.
