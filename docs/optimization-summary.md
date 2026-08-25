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
| Section 4 | not filed |

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
| Ed25519 instead of ECDSA | 325,600 → 374,400 tps — this removed the *generator's* ceiling, not the committer's; Go's ECDSA draws a nonce per signature through `getrandom`, which serialised at ~35 concurrent callers | config |
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

## 5. Current constraint — identified, not fixed

| # | Constraint | Evidence |
|---|---|---|
| 5.1 | **Verifier batching cuts on time before a batch fills** — `batch-time-cutoff` is 2 ms while a 500-transaction batch takes 2.95 ms to fill at this rate | batches are cut at roughly 340 transactions, and split across `parallelism: 128` that is under three signatures per goroutine per dispatch; the verifier machines sit at 34% CPU while holding the only non-empty queue in the pipeline (`coordinator_verifier_input_batch_queue_size` at 58, every other queue zero) |

## Where the pipeline stands

| | Throughput | Note |
|---|---|---|
| Start of the evaluation | 80,000 tps | |
| After sections 1-4 | **500,000 tps** requested and delivered, 99.9%, 645 ms mean | sustainable-rate hold, sidecar in the path |
| Same deployment, over-driven | 510,371 mean / 528,800 peak | asking 1,000,000; latency is then buffer occupancy, 8-9 s, and says nothing about the pipeline |
| Coordinator-direct, sidecar out of the path | 525,388 mean / 533,213 peak | so the sidecar's throughput cost is within measurement noise; it costs latency |

Every figure above depends on how full the state database was and on the day it was taken: this
cluster's baseline moves about 15% between days with identical code, which is recorded in
`cluster-optimization-log.md` section 6.4 along with what that cost.
