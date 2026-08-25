<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Performance Tuning Guide

This guide explains how each configuration parameter affects system performance — throughput, latency, memory, and pipeline flow. All parameters are documented with their sample values in `cmd/config/samples/`.

## Table of Contents

1. [Pipeline Flow Control](#1-pipeline-flow-control)
2. [Identifying the Bottleneck](#2-identifying-the-bottleneck)
3. [Sidecar](#3-sidecar)
4. [Coordinator](#4-coordinator)
5. [Verifier](#5-verifier)
6. [Validator-Committer (VC)](#6-validator-committer-vc)
7. [Query Service](#7-query-service)
8. [Database](#8-database)
9. [Co-location Impact](#9-co-location-impact)
10. [Benchmarking](#10-benchmarking)

## 1. Pipeline Flow Control

The commit pipeline processes transactions through a sequence of stages connected by bounded channels and slot-based limits. These flow controls don't just protect memory — they directly control how much work flows through the pipeline.

```
Orderer → Sidecar → Coordinator → Dependency Graph → Verifier → VC → Database
             ↑            ↑                                      |
             └── status ──└──────────── status ──────────────────┘
```

Each `→` is a bounded channel or gRPC stream. The system uses three types of flow control:

- **Slot-based limits** act as per-transaction semaphores. Slots are acquired before processing and released only when transactions complete downstream. When exhausted, channels fill up and the Sidecar stops pulling blocks.
- **Channel buffers** connect adjacent stages within a process. When full, the producer blocks.
- **gRPC flow control** operates at the transport layer between services. gRPC uses HTTP/2 flow
  control windows — when a receiver is slow, its window fills up and the sender is blocked from
  writing more data. This prevents a fast producer (e.g., Sidecar) from overwhelming a slow consumer
  (e.g., Coordinator) at the network level, independent of the application-level slot and channel
  limits.

  **This one is sized deliberately, and the defaults were once the whole pipeline's ceiling.**
  `connection.InitialWindowSize` and `InitialConnWindowSize` (`utils/connection/client.go`) set 16 MB
  per stream and 32 MB per connection, applied to clients when dialling and to servers in
  `utils/serve`. Both ends need it: the window a peer may write into is the one this side advertises.
  Note that setting them at all disables gRPC's own BDP-based window auto-tuning, so they are chosen
  generously rather than tightly.

  Without them, a saturated nineteen-machine cluster had *every* coordinator sender to the signature
  verifiers, and five of six to the validator-committers, blocked in
  `transport.(*writeQuota).get` — out of stream send quota — each using a quarter of a core while the
  verifiers they feed sat at 34% CPU holding 1,700 transactions of a 128,000 capacity. Raising the
  windows was worth 13% throughput and cut latency at a fixed 500,000 tps from 645 ms to 392 ms.
  They are credit limits rather than allocations, so they add no buffering at operating rates.

  The symptom to look for is a sender blocked in `writeQuota.get` in a goroutine dump while the
  receiving service has spare CPU and empty queues. No CPU profile shows it, because the sender is
  not running.

Setting any limit too low starves the pipeline — stages run in lock-step rather than streaming, and throughput drops. Setting limits too high increases memory usage and queuing latency. The goal is finding the balance where the pipeline has enough in-flight work to sustain throughput without excessive queuing.

## 2. Identifying the Bottleneck

Monitor queue length gauges to find the bottleneck. A growing queue means the downstream stage cannot keep up. Tune that stage first. The full list of queue metrics and other observability metrics can be found in the [Metrics Reference](metrics_reference.md). Key queues to watch:

| Queue Metric | Stage | Growing Queue Means |
|-------------|-------|---------------------|
| `sidecar_relay_input_block_queue_size` | Block Ingestion | Coordinator not consuming blocks fast enough |
| `sidecar_relay_mapped_block_queue_size` | Block Mapping | Blocks are mapped faster than the coordinator accepts them |
| `sidecar_relay_waiting_transactions_queue_size` | Relay | Transactions waiting for commit statuses to return |
| `sidecar_relay_output_committed_block_queue_size` | Committed Blocks | Committed blocks backing up; downstream consumers slow |
| `sidecar_notifier_input_block_queue_size` | Notification | Notifier not keeping up with committed blocks |
| `sidecar_notifier_input_status_queue_size` | Notification | Notifier not keeping up with status updates |
| `coordinator_verifier_input_batch_queue_size` | Signature Verification | Verifiers cannot keep up; add instances or CPU |
| `coordinator_verifier_output_batch_queue_size` | Verified → VC | VC services not consuming verified transactions fast enough |
| `coordinator_vcservice_output_batch_queue_size` | VC → Dep Graph | Dependency graph not processing validated results fast enough |
| `coordinator_vcservice_output_tx_status_batch_queue_size` | Status Response | Status responses backing up between VC and Coordinator |
| `vcservice_preparer_input_queue_size` | VC Preparation | Preparer workers saturated |
| `vcservice_validator_input_queue_size` | VC Validation | DB validation queries too slow; check connections or co-location |
| `vcservice_committer_input_queue_size` | VC Commit | DB commit throughput is the bottleneck; most common |
| `vcservice_txstatus_output_queue_size` | VC Status Output | Status responses backing up; Coordinator not consuming fast enough |

## 3. Sidecar

### `waiting-txs-limit`

Maximum number of transactions the Sidecar has sent to the Coordinator and is awaiting status for. The Sidecar acquires one slot per transaction before sending a block. Slots are released only when the Coordinator returns the transaction's final status. When all slots are occupied, the Sidecar blocks on slot acquisition, which causes the internal block channel to fill up, and eventually the Sidecar stops pulling blocks from the ordering service.

This value directly controls how many transactions can be in-flight across the entire pipeline. With a low value (e.g., 100), the Sidecar sends 100 transactions and blocks until all complete — the pipeline runs in lock-step and throughput drops dramatically. With a very high value, more transactions queue across the pipeline, increasing memory and queuing latency. Config blocks trigger a full drain regardless of this value. Default: 20,000,000.

### `ledger.sync-interval`

How often the block store calls `fsync` to durable storage. Every Nth block triggers a full sync; intermediate blocks are written without fsync. Config blocks and file rollovers always sync.

Each fsync is an expensive I/O operation. A low value (e.g., 1) syncs every block, which can bottleneck the block ingestion path. Higher values (100, 500+) significantly improve block append throughput by amortizing I/O cost. The tradeoff is durability — blocks lost on crash are recoverable from the ordering service. Default: 100.

### `ledger.disable-tx-id-index`

Drops the block store's transaction ID index, which `GetBlockByTxID` and `GetTxByID` use to find a transaction. Both queries fail once it is off.

This is the block store's dominant cost at high transaction rates, because it writes one index entry per transaction rather than per block, and its LevelDB compaction rewrites those entries as the index grows. On a cluster committing around 100,000 tx/s, a CPU profile of a saturated Sidecar attributed 35% of its samples to that compaction, with the index at 33 GB against 118 GB of blocks; committed throughput fell by roughly a third over an hour at a fixed offered rate as the index kept growing. A deployment that serves neither query is better off without it. Note that the index also selects the block store's on-disk format, so this setting can only be changed against an empty ledger directory. Default: false.

### What limits the sidecar on its own: `BenchmarkSidecarEndToEnd`

`BenchmarkSidecarEndToEnd` (`service/sidecar/sidecar_bench_test.go`) drives the whole service on
one machine — a block arrives over the orderer's Deliver stream and has its consenter signatures
verified, is mapped, is submitted to the coordinator over a real gRPC stream, has its statuses
collected, and is appended to a real on-disk ledger. The orderer and the coordinator are stubbed,
because both sit on other machines in a deployment.

Measured on a 64-core sidecar host with the ledger on a local NVMe disk, 3,000,000 transactions per
point, `disable-tx-id-index` on. Each column adds to the one before it:

| configuration (`blockSize=5000`) | `GOGC=100` | `GOGC=1000` | allocs/tx |
|:---------------------------------|-----------:|------------:|----------:|
| baseline                         |    301,000 |           — |        61 |
| + parallel mapping               |    326,000 |     486,000 |        61 |
| + allocation reductions          |    364,000 |     484,000 |        56 |
| + `fabric-x-common` block store  |    427,000 |     626,000 |        46 |

Three things decide the number, and they have to be fixed in this order — each one is invisible
until the one before it is gone.

**Block size, first and always.** Below about 1,000 transactions the sidecar cannot reach half its
throughput at any setting, because two of its costs are per block rather than per transaction:
verifying the block's consenter signatures, and one `fsync` every `sync-interval` blocks. Block
signature verification alone measures 169,000 tx/s at 100 transactions per block against 1,281,000
at 10,000 (`BenchmarkVerifyBlock` in `utils/deliverorderer`). Nothing else in this section matters
until the blocks are wide.

**The sidecar is allocation-bound, not CPU-bound.** At the default `GOGC=100` its allocation rate
keeps a collection running almost continuously: the whole service used 331% of 6,400% available CPU
while no single stage was busier than 35%. Throughput tracks the allocation rate closely — roughly a
percent of throughput per percent of allocations removed — so the two ways to spend it are reducing
allocations and raising `GOGC`, and because both buy the same thing they do not compound: at
`GOGC=1000` the allocation reductions below make no measurable difference at all.

Where the allocations are, measured with `-memprofile` and attributed per stage (per transaction,
`blockSize=5000`, after the reductions described here):

| site | allocs/tx | note |
|:-----|----------:|:-----|
| `serialization.UnmarshalTx` | 15.8 | decoding each TX; 61% of the sidecar's total |
| status batch unmarshal on the way back | 3.0 | |
| `serialization.UnwrapEnvelopeLite` | 2.1 | |
| block store append | 2.1 | 12.6 before the `fabric-x-common` change below |
| block delivery | 1.1 | |
| remainder of mapping | 1.7 | per-block slices |
| **sidecar total** | **25.8** | |
| the benchmark's own coordinator stub | 20.0 | not the sidecar; see below |

Two of those were removed outright and cost nothing to keep off: `verifyTxForm` allocated 2.8 per
transaction in `checkKeys` — a map and the slice its keys were copied into, per namespace — and now
allocates none, and `TxRef` and `TxWithRef` are allocated once per block as slabs rather than twice
per transaction. Together those were worth 12% at the default `GOGC`.

What is left is dominated by `UnmarshalTx`, and it cannot be removed the way the others were. The
sidecar decodes every transaction into an `applicationpb.Tx` and hands it straight to gRPC to
re-encode, because `TxWithRef.Content` is a message; it needs the decode only to validate the
transaction's form. Pooling those objects is not an option — they are queued per
`StreamAllTransactions` subscriber and outlive the block's commit by an unbounded amount, so reusing
them would be a use-after-free. Removing this cost means changing `TxWithRef` to carry the
transaction's original bytes, so the sidecar validates by scanning the wire format (as
`UnwrapEnvelopeLite` already does for envelopes) and forwards what it received. That is a decision
for the coordinator, verifier and validator-committer as much as the sidecar, since all four read
`Content`.

Note the last row when reading any `GOGC=100` figure here. The benchmark runs the sidecar and its
stubbed coordinator in one process, so they share a heap and a collector, and the stub allocates
almost as much as the whole sidecar — it decodes every transaction's content just to answer with a
status. A deployed sidecar, whose coordinator is on another machine, collects against roughly half
this benchmark's allocation rate, so these figures understate it.

If `GOGC` is raised anyway, set `GOMEMLIMIT` alongside it as a backstop rather than raising `GOGC`
unbounded, and size it from the sidecar's live heap, which `waiting-txs-limit` bounds — not from
this benchmark, which holds its whole transaction pool in memory and so reports a far larger
resident set than a deployment has.

**Mapping is parallel within a block, and only pays once the ledger is cheap.** `mapBlock` parses
and validates a block's messages across up to 16 goroutines, then folds them into the block one at a
time in message order, so the TX ID dedup set and the batch order stay single-threaded and the
outcome does not depend on how the parsing was split. That made mapping itself 5.3x faster
(408,000 to 2,308,000 tx/s, `BenchmarkMapBlockSize`) but was worth *nothing* end to end — 334,000
against 356,000 — until the block store stopped being the binding stage. Afterwards the same change
was worth 26% (497,000 to 628,000). A faster stage behind a saturated one buys nothing; this is the
clearest example of it in the repository.

The block store is the remaining bottleneck, and the fix is not in this repository.
`blkstorage.serializeBlock` calls `protoutil.GetOrComputeTxIDFromEnvelope` for every envelope, which
unmarshals `Envelope`, then `Payload`, then `ChannelHeader`, purely to fill a `txindexInfo.txID`
that is discarded when the transaction ID index is off. It measured 92% of the block store's CPU and
2.04 µs per transaction, capping the pipeline near 476,000 tx/s however fast everything else gets.
It is unconditional in `serializeBlock`, so no sidecar setting avoids it. Skipping it when
`isAttributeIndexed(IndexableAttrTxID)` is false — a change to `fabric-x-common` — removed 10
allocations per transaction and was worth 25%. The figures in the third column above include it.

#### The same finding on a nineteen-machine cluster

Everything above is one machine with the coordinator stubbed, so it is worth recording what the
block store did in a real deployment: sidecar on its own 64-core host, coordinator and three
verifiers and six validator-committers and a twelve-node YugabyteDB behind it, 10,000-transaction
blocks, `disable-tx-id-index` on.

The sidecar looked idle and was the bottleneck anyway. At the ceiling it used 18% of its cores, 3.6
of 156 GB, and its disk was 4% busy at 157 MB/s — because the stage that was saturated is a single
goroutine, and one pinned core out of sixty-four is 1.6% of the machine. What identifies it is not
CPU but the duty cycle of each per-block stage against the per-block budget:

| stage | before | after | budget at 450,000 tx/s |
|:------|-------:|------:|-----------------------:|
| `sidecar_ledger_append_block_seconds` | 22.2 ms | 7.1 ms | 22.2 ms |
| `sidecar_relay_block_mapping_seconds` | 9.9 ms | 8.8 ms | 22.2 ms |
| `sidecar_relay_mapped_block_processing_seconds` | 7.5 ms | 6.9 ms | 22.2 ms |
| `sidecar_delivery_block_verification_seconds` | 3.0 ms | 3.1 ms | 22.2 ms |

An append at 22.2 ms of a 22.2 ms budget predicts 10,000 / 22.2 ms = 451,000 tx/s, and the measured
knee was just above 450,000. Both columns were measured with the sidecar's `waiting-txs-limit` at
300,000, which is itself a throughput limit — see the end of this subsection before comparing either
figure with anything. A 30-second CPU profile of the live process attributed 7.4% of the whole
sidecar and about 88% of `appendBlock` to `addDataBytesAndConstructTxIndexInfo`, over half of that in
the `proto.Unmarshal` behind `GetOrComputeTxIDFromEnvelope`. `runtime.gcDrain` was 57% of all CPU,
which is the allocation-bound picture above showing up unchanged at cluster scale.

With the append fixed the whole pipeline held 480,689 tx/s over a five-minute window on a fresh
deployment, peaking at 483,600, and the sidecar was no longer what stopped it: 13% CPU, every
internal queue empty, no stage above 60% of its budget. What stopped it was the database commit, and
its arithmetic closes — 6 validator-committers × 32 committer workers = 192 concurrent commits at
139 ms each and 341 transactions a batch is 471,000 tx/s.

That figure is not evidence of a sidecar cost, and reading it as one is the mistake to avoid here.
The comparable coordinator-direct numbers in `cluster-optimization-log.md` were all taken with the
coordinator's `dep-graph-wait-tx-limit` of 500,000 as the pipeline's in-flight window. Put the
sidecar in front with a `waiting-txs-limit` below that, and the sidecar's window silently replaces
the coordinator's as the binding one — and since throughput is in-flight over latency, a smaller
window caps throughput and not merely latency. That log prices the knob: 470,594 tx/s at a 200,000
window and 486,941 at 500,000. The 480,689 above was measured at 300,000 and sits on that same
curve.

Matching the two windows settles it. With both at 500,000 and the same 1,000,000 tps request that
produced the best coordinator-direct figure, the full pipeline through the sidecar held 523,316 tx/s
over forty-five minutes and peaked at 548,800, against 525,388 and 533,213 coordinator-direct — the
means within 0.4% and the peak higher through the sidecar. Mean latency went from 1,050 ms to
5,400 ms, which is what an extra stage and a deeper buffer are supposed to cost. Section 6.2 of
`cluster-optimization-log.md` has the run.

So keep `waiting-txs-limit` at or above the coordinator's `dep-graph-wait-tx-limit`. Sizing it below
that does not make overload more visible; the coordinator's window already does that, and the mock
orderer's ring bounds what can queue in front of the sidecar.

Two lessons transfer. A per-block stage's duty cycle is the diagnostic, not machine CPU — a
saturated single goroutine is invisible in every utilisation graph. And block size sets the budget:
at 10,000 transactions a block, 450,000 tx/s allows 22 ms per stage, which is generous enough that a
stage has to be badly wrong to breach it, and the append was.

### `channel-buffer-size`

Buffer size for internal Go channels in the Sidecar — block delivery, committed blocks, and status updates. When a channel is full, the producing goroutine blocks until the consumer reads.

A small buffer (e.g., 1-10) tightly couples the Sidecar's internal stages: any slowdown in the relay to the Coordinator immediately stalls block delivery from the orderer. A larger buffer absorbs temporary throughput variations but uses more memory. Default: 100.

### `last-committed-block-set-interval`

How often the Sidecar sends the latest committed block number to the Coordinator. The Coordinator uses this for dependency resolution.

Minimal effect on steady-state throughput. Shorter intervals improve recovery speed after failures. Default: 5s.

### `notification.max-active-tx-ids`

Global limit on active transaction ID subscriptions across all notification streams. When exhausted, new subscriptions are partially rejected.

Too low causes clients to receive rejections under moderate load, forcing retries that increase end-to-end latency. Default: 100,000.

### `notification.max-tx-ids-per-request`

Maximum transaction IDs per single notification request. Requests exceeding this are rejected entirely.

Prevents individual clients from consuming a disproportionate share of the subscription budget in a single call. Default: 1,000.

### `server.max-concurrent-streams`

Maximum concurrent streaming RPCs (Deliver + Notification) per client connection. Each stream holds server resources (goroutines, buffers).

Too low limits client concurrency and can cause connection failures under load. Default: 10.

## 4. Coordinator

### `dependency-graph.num-of-local-dep-constructors`

Number of goroutines that process transaction batches in parallel to construct batch-level dependency graphs. Each worker processes one batch at a time, and output is serialized in FIFO order.

Increasing this parallelizes the CPU work of building dependency graphs, but measurement says it does not raise throughput at all. `BenchmarkDependencyGraph` over the no-dependency shape on a 32-core machine, 300,000 transactions per case, gives 216,886 / 249,691 / 220,000 / 246,929 / 221,484 / 228,068 tx/s at 1 / 2 / 4 / 8 / 16 / 32 constructors: no trend, and the spread is scheduling noise rather than a curve. The reason is that the pool is not what bounds the default manager -- the global manager's single graph goroutine and single validated-batch goroutine are, and no number of constructors widens them. Output order is also enforced through a condition variable, so a constructor may only run a bounded distance ahead of the last batch released. Raise this only if a profile shows the constructors themselves saturated. Default: 1.

### Pre-splitting the state tables: a read/write trade-off worth measuring

Not a committer setting, but the deployment choice with the largest measured effect on this project's
cluster, and one that is easy to get wrong in a way nothing reports.

The state tables are created with `SPLIT INTO N TABLETS` on YugabyteDB (`${SPLIT_INTO_TABLETS}` in
`utils/statedb/create_namespace_tmpl.sql`). A tablet is the unit of write concurrency as well as of
placement, so one tablet per tablet server lets a 64-core machine commit to only one Raft group per
table at a time, and raising the count gives each server something to interleave.

It also destroys read batching. With the table split into 120 tablets, `WHERE key = ANY($1)` issues
one storage read request **per key** rather than one per tablet. Measured on identical tables with
identical rows and the same 1,200 existing keys, differing only in the split:

| Split | Storage read requests | Execution time |
|---|---|---|
| `SPLIT INTO 120 TABLETS` | 1,200 | 7,801 ms |
| Default | 2 | 12.5 ms |

A factor of 622, and only 440 ms of the 7,801 is storage work — the rest is 1,200 serialised round
trips. The query plan is a correct primary-key index scan in both cases, so nothing in `EXPLAIN`
short of the `DIST` request counts reveals it.

This stayed invisible for as long as the workload only inserted fresh keys, because nothing then
performs a multi-key lookup. It appears the moment anything does. On this cluster it took the
blind-write path -- `queryVersionsIfPresent`, which the validator uses to decide insert versus update
(`populateVersionsAndCategorizeBlindWrites`) -- from negligible to 6.3 seconds per batch, which was
99.8% of the entire commit path's time and dropped throughput from 486,941 tps to 13,160. The query
service and read validation take the same shape and would be affected the same way.

**What actually governs it is the product of tablets and keys per lookup, not the tablet count.**
Batching survives while `tablets × keys-per-lookup` stays below roughly 32,768 and collapses to one
request per key above it. Measured on this cluster, with the last four rows run as predictions of the
model rather than as fits to it:

| Tablets | Keys | Product | Storage read requests |
|---|---|---|---|
| 24 | 1,200 | 28,800 | 1 |
| 28 | 1,200 | 33,600 | 4 |
| 29 | 1,200 | 34,800 | 1,200 |
| 120 | 1,200 | 144,000 | 1,200 |
| 24 | 5,000 | 120,000 | 5,000 |
| 12 | 2,000 | 24,000 | 1 |
| 12 | 2,600 | 31,200 | 2 |
| 12 | 3,000 | 36,000 | 3,000 |
| 6 | 5,000 | 30,000 | 3 |

Of the two terms in that product, **only the key count is a durable lever.** The tablet count is not
something a deployment controls for long: YugabyteDB splits tablets automatically as a table grows, so
`SPLIT INTO N TABLETS` sets a starting point rather than a ceiling. On this cluster a table created
with 120 tablets held 288 after eleven hours of load, and `tx_status` had gone from 120 to 212. A table
created with 12 will pass 29 on its own, at which point the batching breaks whatever the operator
chose.

So the budget shrinks over the life of a deployment without anyone changing a setting. At 288 tablets
it is about 114 keys per lookup, which at two writes per transaction is roughly 57 transactions per
committed batch. Sizing the committed batch to respect the product is therefore the fix that lasts;
lowering the initial tablet count only postpones the problem.

The constant is inferred on this cluster against YugabyteDB 2025.2.1.0 and should not be treated as
portable. The product relationship is what to test, and `Storage Read Requests` from
`EXPLAIN (ANALYZE, DIST)` tests it in a single query.

Both sides of the tablet trade-off are real, so this is a curve to be tuned rather than a setting
with a right answer. The same cluster, measured over 300-second windows from clean deployments:

| Split | Insert-only workload | Workload with a multi-key read |
|---|---|---|
| 120 tablets (10 per server) | **486,941 tps** | 13,160 tps |
| 48 tablets (4 per server) | — | 31,132 tps |
| 12 tablets (1 per server) | 359,866 tps | **314,336 tps** |

120 tablets is worth 35% on the insert-only workload, exactly the write concurrency it was chosen
for, and costs a factor of 24 on anything performing a multi-key read. 12 tablets is balanced.
Neither is simply better, and a deployment tuned only against an insert-only benchmark will pick 120
and then fall off a cliff the first time a client reads or updates existing state.

Read the 12-tablet row as a starting point and not as a setting that holds, for the reason above: the
table splits its way past the batching threshold as it grows, so the low tablet count buys time rather
than a fix.

If a deployment raises the tablet count for write concurrency, measure a multi-key read before and
after, and read `Storage Read Requests` from `EXPLAIN (ANALYZE, DIST)` rather than the plan shape.

### `dependency-graph.use-simple-manager`

Selects the simple dependency graph manager. The default manager splits the work between a pool of local dependency constructors and a global graph guarded by one mutex; the simple manager keeps the whole waiting set in a single map owned by one goroutine, fed by channels, with no lock at all. `num-of-local-dep-constructors` has no effect when it is enabled.

Compare `coordinator_global_dependency_graph_validated_tx_batch_processing` against `..._validated_tx_batch_processor_wait_for_lock` and `..._constructor_wait_for_lock` to see how much of the graph's busy time is contention rather than work. On this project's 19-machine cluster the validated batch processor reached 95% utilisation with 40% of it waiting for the mutex, and the constructor 86% with 43% waiting, on a 64-core machine whose busiest thread was under 20%.

Enabling the simple manager removed that contention entirely — those stages disappear from the utilisation
sweep — but on first measurement throughput did not change, because the pressure it released was absorbed by
the next stage down: database commit latency rose from 90 ms to 120 ms at the same committed rate. Treat a
lock-contention reading as a reason to check what is behind the lock, not as a throughput gain on its own.

Later measurement, after the manager's output path was fixed so that it no longer blocked (see the halt note
below), shows it substantially faster. `BenchmarkDependencyGraph` over the no-dependency shape gives
321,899 tx/s against 249,691 for the best default-manager configuration, a 29% gain in the same harness over
the same transaction count. On the 19-machine cluster, with both managers measured under an identical
configuration from a fresh deployment over a 300-second window, it is larger:

| | Committed | Mean latency | In-flight | DB batch commit | Busiest host CPU |
|---|---|---|---|---|---|
| Simple manager | **486,941 tps** | 1,099 ms | 558,315 | 140 ms | 76% |
| Default manager | 329,854 tps | 1,596 ms | 523,336 | 62 ms | 60% |

47.6%, and the mechanism is in the same row rather than inferred: under the default manager the database is
**starved**. Batch commit latency falls from 140 ms to 62 ms and the commit machines drop from 76% to 60% CPU
while the graph sits full at 523,336 transactions in flight. Work is queued in the coordinator and the
machines that would do it are idle, which is what a constraint in the graph looks like from outside.

Two cautions on that figure. Both rows have `key-backref-rate` at 0, so no two transactions touch the same
key: the graph is tracking transactions that cannot conflict, which is the case that favours holding the
waiting set in one goroutine. And an earlier cluster comparison put the gain at 41% by measuring the default
manager while the load generator was itself the limit at about 358,000 tps — a floor rather than a ceiling.
Do not compare runs taken either side of a change to the generator.

Two defects had to be fixed before the simple manager could sustain load, and both are worth knowing about if
this is enabled. It held the members of every key's running group rather than counting them, so under
sustained load — where the namespace key every transaction reads never has an idle instant — it retained every
transaction it had ever seen, growing with transactions committed rather than transactions waiting. And its
single goroutine both wrote the output and drained the validated input, which closes the coordinator's queue
ring: it blocked on a full output, so it never took the validated batch that would have let that output drain.
Nothing errored and no goroutine died; the pipeline simply stopped, after 35-45 million transactions.
`drain_test.go` covers both. Default: false.

### `dependency-graph.waiting-txs-limit`

Maximum number of transactions in the global dependency graph. The Coordinator acquires one slot per transaction before adding it to the graph. Slots are released when the VC returns validation results. When exhausted, the dependency graph construction blocks, channels fill up, and the Sidecar stops pulling blocks.

The dependency graph is what enables parallel dispatch to Verifier and VC services. A small graph (e.g., 100) means once transactions are dispatched, no new ones enter until results return — creating idle gaps and reducing throughput. A very large graph increases memory for dependency tracking state and queuing latency. Incoming blocks are chunked into batches of `min(waiting-txs-limit, 500)` to prevent a single block from consuming all slots. Default: 100,000.

Oversizing it is not free, and the cost is larger than "increases memory" suggests. On this project's
19-machine cluster, going from 20,000,000 to 500,000 left the committed rate unchanged at roughly 500,000 tps
while mean latency fell from 2,478 ms to 691 ms and the coordinator's resident memory fell from 79 GB to
786 MB — about 4 KB per waiting transaction, and 100x less memory for the same throughput. (Resident
memory is a high-water mark and only tracks the limit while in-flight is actually reaching it; between
200,000 and 500,000 the difference is inside measurement noise.) The 20M limit
bought nothing: it only let 1.25 million transactions queue where 300,000 sufficed, and Little's law turns
that excess directly into latency. Size it from the transactions actually needed in flight to keep the
validator-committers busy, which is the bandwidth-delay product of the pipeline, not from how many the
machine could hold.

Note also that this limit only becomes load-bearing once nothing upstream blocks first; it had no observable
effect until the simple manager stopped blocking on its output channel.

### `per-channel-buffer-size-per-goroutine`

Base buffer size for internal Go channels connecting the Coordinator's pipeline stages. The actual buffer for each channel is computed as base × number of endpoints (or constructors):

```
Coordinator → DepGraph:  base × num-of-local-dep-constructors
DepGraph → Verifier:     base × number-of-vc-endpoints
Verifier → VC:           base × number-of-verifier-endpoints
VC → DepGraph:           base × number-of-vc-endpoints
VC → Coordinator:        base × number-of-vc-endpoints
```

When a channel is full, the producing stage blocks. A small buffer means any momentary downstream slowdown immediately stalls all upstream stages. A larger buffer absorbs temporary throughput variations but increases memory and queuing latency. With 3 verifiers and 6 VCs, the defaults produce 30-60 item buffers per channel. Default: 10.

## 5. Verifier

### `parallel-executor.parallelism`

Number of goroutines that verify signatures in parallel. Signature verification is CPU-bound. Each stream from the Coordinator creates an independent executor with this many workers.

Set this to match the number of CPU cores available on the Verifier node. Under-setting leaves CPU idle, reducing verification throughput and causing transactions to queue at the Coordinator. Over-setting causes context switching overhead with no throughput gain. The default of 40 assumes a 32+ core machine.

### `parallel-executor.batch-size-cutoff`

Minimum number of verification results to collect before emitting a batch to the Coordinator. Results are buffered until this threshold is reached or `batch-time-cutoff` expires, whichever comes first.

Setting this too low causes many small batches to be emitted, increasing per-batch overhead in channel writes and gRPC communication. Setting this too high delays results — individual transactions wait longer for the batch to fill, increasing end-to-end latency.

| Setting | Throughput | Latency |
|---------|-----------|---------|
| 10-20 | Lower (more frequent, smaller batches) | Lower |
| 50 (default) | Good balance | Moderate |
| 100-200 | Higher (fewer, larger batches) | Higher |

### `parallel-executor.batch-time-cutoff`

Maximum time to wait for a batch to reach `batch-size-cutoff` before emitting a partial batch. This is the latency safety valve — without it, a partially filled batch under low load would wait indefinitely.

Setting this too low defeats batching (batches emit before they can fill). Setting this too high adds latency during periods of low or variable transaction arrival rates.

The default of 10ms ensures results are emitted promptly even under low load. Increase to 50-100ms only if batching efficiency is more important than latency.

### `parallel-executor.channel-buffer-size`

Buffer size for internal channels in the verification pipeline. The actual channel capacity is computed as:

```
capacity = channel-buffer-size × parallelism
```

With the defaults (50 buffer × 40 parallelism = 2,000 capacity). When the input channel is full, the Coordinator's dispatch to the Verifier blocks, which stalls the dependency graph. When the output channel is full, verification workers block, reducing effective parallelism.

Setting this too low causes frequent blocking that reduces the effective parallelism of the verification workers. Setting this too high increases memory usage proportionally. The defaults provide enough buffering for sustained high throughput.

## 6. Validator-Committer (VC)

The VC processes transactions through three pipeline stages: preparation, validation, and commit. Each stage has an independent worker pool. The overall throughput is limited by the slowest stage.

### `resource-limits.max-workers-for-preparer`

Number of goroutines that extract read/write sets from transactions and organize them for validation. Preparation is **CPU-bound** — it parses transaction payloads, builds namespace-to-reads maps, and categorizes writes.

Setting this too low makes the preparer the bottleneck — transactions queue up waiting for preparation while the validator and committer workers sit idle. Setting this higher than needed wastes CPU on goroutine overhead.

The default of 1 is sufficient for most workloads because preparation is fast relative to the database-bound stages. Increase to 2-4 if you observe the preparer queue growing while validator and committer queues are empty.

### `resource-limits.max-workers-for-validator`

Number of goroutines that perform MVCC validation against the database. Each worker calls a stored procedure (`validate_reads_ns_<namespace>`) that checks whether read set versions still match the committed state.

Validation is **database-bound** — each call makes at least one database round-trip per namespace in the transaction's read set. Each active validator worker holds a database connection for the duration of its query.

Setting this too low causes transactions to queue at the validator stage while the database has spare capacity. Setting this too high exhausts the connection pool — workers compete for connections, and the overhead of connection acquisition negates the parallelism benefit.

| Setting | Effect |
|---------|--------|
| 1 (default) | Serialized validation; minimal DB load; may under-utilize DB |
| 2-4 | Parallel validation; higher throughput; needs more DB connections |
| >4 | Diminishing returns unless DB can sustain the concurrent queries |

### `resource-limits.max-workers-for-committer`

Number of goroutines that commit validated transactions to the database using stored procedures (`insert_tx_status`, `insert_ns_<namespace>`, `update_ns_<namespace>`). Each commit involves multiple database round-trips within a transaction: writing transaction status, inserting new keys, and updating existing keys.

The committer is typically the **pipeline bottleneck** because it performs the most database work per transaction. Each active committer worker holds a database connection for the duration of the commit. When transactions complete here, slots are released in the Coordinator's dependency graph and the Sidecar's waiting-txs pool — so committer throughput directly controls how fast the entire pipeline can flow.

Setting this too low starves the pipeline — the Coordinator's dependency graph fills up, backpressure reaches the Sidecar, and block ingestion stalls. Setting this too high overwhelms the database with concurrent transactions, causing contention and increased retry rates.

| Setting | Effect |
|---------|--------|
| 1-2 | Low throughput; database under-utilized; pipeline stalls |
| 10-20 (default: 20) | Good throughput; recommended starting point |
| >20 | Diminishing returns; may overwhelm the database with contention |

### `resource-limits.min-transaction-batch-size`

Minimum number of transactions that must accumulate before the batch is forwarded to the preparation stage. The VC waits for this many transactions or until `timeout-for-min-transaction-batch-size` expires, whichever comes first. Config blocks are always sent immediately regardless of batch size.

Larger batches improve efficiency — stored procedures process keys in bulk, reducing per-transaction overhead. But larger batches also increase latency because early-arriving transactions wait for the batch to fill.

Setting this too high under low transaction rates causes transactions to sit idle until the timeout expires, adding `timeout-for-min-transaction-batch-size` of latency to every batch. Setting this to 1 (default) disables batching entirely — every transaction is forwarded immediately for lowest latency.

| Setting | Throughput | Latency |
|---------|-----------|---------|
| 1 (default) | Lower (no batching benefit) | Lowest |
| 10-50 | Higher (bulk stored procedure operations) | Higher (waits for batch to fill) |

### `resource-limits.timeout-for-min-transaction-batch-size`

Maximum time to wait for a batch to reach `min-transaction-batch-size`. This is the latency safety valve for batching — without it, a partially filled batch under low load would wait indefinitely.

When `min-transaction-batch-size` is 1, this timeout has no effect (batches are sent immediately). When `min-transaction-batch-size` is higher, this timeout determines the worst-case additional latency during low-throughput periods.

The default of 2s pairs with the default batch size of 1 (effectively unused). If you increase the batch size, consider reducing the timeout to 100-500ms to bound the latency impact.

### `resource-limits.max-workers-for-snapshot-hash`

Number of goroutines that hash namespace tables in parallel when computing a snapshot's hash. Hashing runs in the background, after the snapshot transaction is committed, against a clone database of the snapshot — so it never blocks the commit pipeline, but it does add read load to the cluster.

The snapshot hash worker opens its own short-lived connection pool against the clone database, sized to `max-workers-for-snapshot-hash`. It therefore does not consume the main pool's connection budget (see `database.max-connections`), but the cluster must have headroom for those extra connections and the scan traffic they generate.

| Setting | Effect |
|---------|--------|
| 1 | Serialized per-table hashing; lowest read load; slowest hash |
| 4 (default) | Parallel per-table hashing; good balance |
| >4 | Faster hashing on many-namespace deployments; competes with live traffic |

### `resource-limits.snapshot-hash-batch-size`

Number of rows fetched per round-trip when scanning a table for hashing (keyset pagination). Larger batches reduce the number of round-trips but increase the memory held by each hashing worker; total memory scales with `max-workers-for-snapshot-hash × snapshot-hash-batch-size`, and it also depends on the size of the keys and values in the table being hashed. The default of 1000 keeps per-worker memory bounded on large tables.

### `database.max-connections`

Maximum number of connections in the database connection pool. The validator and committer stages share this pool (the preparer does not use the database — it performs in-memory parsing only). If the pool is exhausted, workers block waiting for a free connection, reducing effective parallelism.

Size the pool to accommodate concurrent usage:

```
Required connections >= max-workers-for-validator + max-workers-for-committer
```

Snapshot hashing does not draw from this pool — it uses its own short-lived pool against the clone database (see `resource-limits.max-workers-for-snapshot-hash`).

Setting this too low causes connection starvation — workers sit idle waiting for connections while the database has spare capacity. Setting this too high wastes database server memory and can cause connection-level contention.

The default of 10 is conservative and likely insufficient for production workloads with the default committer worker count of 20. Size this to at least match the total validator + committer worker count.

### `database.min-connections`

Minimum number of idle connections maintained in the pool. Keeps connections warm to avoid the overhead of establishing new connections (TCP handshake + TLS negotiation + authentication) under sudden load spikes. Set to roughly 50% of `max-connections`.

### `database.load-balance`

Enables client-side load balancing across multiple database endpoints. When enabled, each new connection is distributed across the configured endpoints.

- **`true`**: Required for YugabyteDB clusters to distribute operations across nodes and avoid hotspots. Without this, all connections go to the first endpoint, overloading one node while others sit idle.
- **`false`**: Use for single-node deployments.

### `database.table-pre-split-tablets`

Number of tablets to pre-split each table into at creation time (YugabyteDB only). Pre-splitting distributes data across tablet servers from the start, preventing the "hot tablet" bottleneck where a single tablet handles all initial writes before automatic splitting kicks in.

Without pre-splitting, the first hours of operation can see severe write latency spikes as all writes converge on a single tablet. Pre-splitting eliminates this problem entirely.

Set this to match the number of tablet servers in the cluster, or a small multiple (1x-2x). For example, with 9 tablet servers, set to 9 or 18.

When the database is PostgreSQL, this setting is automatically ignored.

| Setting | Effect |
|---------|--------|
| 0 (default) | No pre-splitting; hot tablet during initial writes; latency spikes |
| = tserver count | Even distribution from the start; predictable latency |
| 2x tserver count | Finer distribution; slightly more per-tablet overhead |

### `database.retry`

Exponential backoff retry strategy for database operations. Required for YugabyteDB to handle retryable transaction conflicts (e.g., serialization failures from concurrent MVCC operations).

| Parameter | Default | Effect |
|-----------|---------|--------|
| `initial-interval` | 500ms | First retry delay |
| `randomization-factor` | 0.5 | Jitter range (+/- 50%) to avoid thundering herd |
| `multiplier` | 1.5 | Exponential backoff factor |
| `max-interval` | 60s | Cap on any single retry delay |
| `max-elapsed-time` | 15m | Total retry duration before giving up |

For high-throughput workloads with frequent transaction conflicts, reduce `initial-interval` to 100-200ms and `max-interval` to 10-30s to retry faster. Too-aggressive retries (very short intervals) can amplify contention under heavy load.

## 7. Query Service

### `min-batch-keys`

Minimum number of keys that must accumulate in a query batch before executing against the database. The batch is submitted when this threshold is reached or `max-batch-wait` expires, whichever comes first.

Batching reduces database round-trips by combining keys from multiple concurrent requests into a single query. Setting this too low sends many small queries, increasing per-query overhead. Setting this too high delays queries waiting for the batch to fill, increasing latency for individual requests.

| Setting | Throughput | Latency |
|---------|-----------|---------|
| 256-512 | Lower (smaller batches, more round-trips) | Lower |
| 1024 (default) | Good balance | Moderate |
| 2048-4096 | Higher (larger batches, fewer round-trips) | Higher |

### `max-batch-wait`

Maximum time to wait for a batch to reach `min-batch-keys`. This bounds the worst-case latency during low-load periods when keys accumulate slowly. Setting this too low defeats batching (queries execute before batches can fill). Setting this too high adds latency during quiet periods.

The default of 100ms is appropriate for most deployments. Reduce to 50ms for lower latency.

### `view-aggregation-window`

Time window for aggregating multiple views with the same parameters (isolation level, deferrable mode) into a single batched view. Views created within this window share the same batcher, reducing database load.

Setting this too low creates many independent batchers, each holding its own database connection — increasing connection pool pressure. Setting this too high delays the first query in a window, as new views must wait for the window to open a batcher.

The default of 100ms balances throughput and latency.

### `max-aggregated-views`

Maximum number of views that can be aggregated into a single batcher. Once reached, a new batcher is created even if within the `view-aggregation-window`. This prevents a single batcher from becoming a contention point under very high concurrency. The default of 1024 is appropriate for most deployments.

### `max-active-views`

Maximum concurrent active views across all clients. New `BeginView` requests are rejected with `RESOURCE_EXHAUSTED` when this limit is reached.

Setting this too low causes clients to receive errors during peak load, forcing retries that amplify latency. Setting this too high allows unbounded resource consumption. Set to 0 to disable the limit (not recommended in production). The default of 4096 is permissive.

### `max-view-timeout`

Maximum lifetime of a view from creation to completion. Views exceeding this timeout are aborted and their queries return errors. This prevents dangling views from holding resources indefinitely.

Setting this too low causes legitimate long-running queries to be aborted. Setting this too high allows idle or abandoned views to hold database connections for extended periods, reducing pool availability for other views.

This parameter interacts with the connection pool. The maximum number of database connections needed by the Query Service depends on:

```
max connections needed = (max-view-timeout / view-aggregation-window) × view-parameter-permutations
```

There are up to 8 view parameter permutations (4 isolation levels × 2 deferrable modes). With defaults: `(10s / 100ms) × 8 = 800`. In practice, not all permutations are used simultaneously, so fewer connections are needed. Monitor connection pool wait metrics to determine the right size.

### `max-request-keys`

Maximum number of keys allowed in a single query request. Applies to both `GetRows` (total keys across all namespaces) and `GetTransactionStatus` (number of transaction IDs). Setting this too low forces clients to split queries into many small requests, increasing round-trip overhead. Setting this too high allows individual requests to cause memory spikes and long-running queries that block the connection pool. The default of 10,000 is a reasonable balance.

### `database.max-connections` (Query Service)

The Query Service connection pool operates differently from the VC pool. Connections are held for the duration of a view's queries, which can be up to `max-view-timeout`. A view that holds a connection for 10 seconds blocks that connection from serving other views.

Size the pool based on expected concurrent view load and the formula above. The default of 10 is conservative — increase for production workloads. Monitor the connection pool wait metrics to detect when views are blocking on connection acquisition.

## 8. Database

### YugabyteDB Considerations

- **Tablet distribution**: Use `table-pre-split-tablets` (VC config) to distribute tablets evenly across all tablet servers. Without pre-splitting, initial writes hit a single tablet, causing latency spikes until automatic splitting occurs.
- **Rebalancing**: After adding new tablet servers, rebalance tablets to distribute data to the new nodes.
- **Connection load balancing**: Enable `database.load-balance: true` on all services connected to YugabyteDB to distribute queries across nodes.

### PostgreSQL Considerations

- **`table-pre-split-tablets`**: Automatically ignored when PostgreSQL is detected.
- **Connection pool**: Same `max-connections` and `min-connections` parameters apply. Size based on concurrent query volume.
- **Replication**: For read-heavy workloads, configure Query Service instances to connect to read replicas.

## 9. Co-location Impact

MVCC validation requires multiple database round-trips per transaction — read set validation, write set application, and status updates. When VC instances are co-located with database nodes, each of these round-trips takes microseconds instead of milliseconds. Without co-location, expect significantly higher commit latency, which directly limits overall system throughput.

Co-location is most impactful for the VC service because it performs the most database operations per transaction. The Query Service benefits less because its read-only queries are less latency-sensitive.

## 10. Benchmarking

The tuning recommendations in this guide are starting points, not guarantees. Real performance depends on factors specific to your deployment:

- Transaction size and complexity (number of read/write keys per transaction)
- Number of namespaces and endorsement policy complexity
- Read set and write set sizes
- Network topology and latency between nodes
- Storage hardware characteristics

Benchmark with your actual workload on your target hardware to establish baseline performance and identify the tuning parameters that matter most for your use case.
