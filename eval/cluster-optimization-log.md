<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Cluster Optimization Log

A record of the changes that took a nineteen-machine deployment from 80,000 to 500,000 committed
transactions per second sustained — 578,383 over-driven — what the evidence for each was, and which of
them turned out to buy nothing. It is a companion to the
[Performance Tuning Guide](../docs/performance-tuning.md): that guide says what each parameter does, this one
says what actually moved on real hardware and how the constraint was located each time.

Three shorter documents draw on this one: [`optimization-summary.md`](optimization-summary.md) for what
each change was worth, [`optimization-config.md`](optimization-config.md) for the assembled
configuration that produced the figures, and [`optimization-issues.md`](optimization-issues.md) for the
issues filed.

The changes that bought nothing are recorded as carefully as the ones that worked. Four of the
six constraints found were not where the first hypothesis put them, and two well-reasoned fixes
removed a real bottleneck without raising throughput at all.

## Table of Contents

1. [The deployment](#1-the-deployment)
2. [Progression](#2-progression)
3. [Changes that raised throughput](#3-changes-that-raised-throughput)
4. [Changes that fixed a real problem but did not raise throughput](#4-changes-that-fixed-a-real-problem-but-did-not-raise-throughput)
5. [The load generator became the limit](#5-the-load-generator-became-the-limit)
6. [Where the constraint is now](#6-where-the-constraint-is-now)
7. [How the constraint was located each time](#7-how-the-constraint-was-located-each-time)
8. [Measuring without fooling yourself](#8-measuring-without-fooling-yourself)

## 1. The deployment

Nineteen machines, each 64 cores and 156 GB, everything running as a host binary:

```
loadgen (mock orderer) -> sidecar -> coordinator -> 3 verifiers -> 6 validator-committers
                                                                      -> YugabyteDB (12 tablet servers, 3 masters)
```

There is no ordering service: the load generator cuts and signs blocks itself and serves them
over the Atomic Broadcast API, so nothing measured here belongs to an orderer. Six of the twelve
database machines also host a validator-committer. Blocks carry 10,000 transactions, and every
transaction writes a fresh unique key, so there are no read-write conflicts and no MVCC aborts —
this measures the pipeline's ceiling rather than its conflict behaviour.

### 1.1 Storage, and what it can actually do

Every machine has a small root volume and **two data volumes**, both XFS, mounted `/data1` and `/data2`.
On the committer machines they are 969 GB each; on the twenty ordering machines, 485 GB each. All are
virtio block devices (`/dev/vdb`, `/dev/vdc`) with the `none` I/O scheduler — there is no `/dev/nvme*` on
any host and `lsblk` reports no device model, so the backing medium is not something this deployment can
observe. Earlier notes here called it local NVMe; that was never verified and the numbers below are what
replace it.

A tablet server is given **one data directory per physical disk**, `/data1/yb-tserver` and
`/data2/yb-tserver`, rather than a stripe underneath it: YugabyteDB then sees the disk boundary and reads
and writes both in parallel, which one directory cannot do however fast the device behind it is. The three
masters use `/data1/yb-master` only and share that disk with the tablet server on the same machine, which
is why no validator-committer is placed on those three.

Measured with fio 3.35, direct I/O, 20 s per job, on a deployed but unloaded cluster:

| job | committer machine | ordering machine |
|---|---|---|
| sequential write, 1 MiB, QD32 | **1,007 MiB/s**, 31.8 ms mean | **504 MiB/s**, 63.6 ms mean |
| random read, 4 KiB, QD64 | 505–600 MiB/s, 129k–154k IOPS, 0.42–0.49 ms | 397 MiB/s, 102k IOPS, 0.63 ms |
| random write, 4 KiB, QD64 | 399 MiB/s, 102k IOPS, 0.63 ms | 190 MiB/s, 49k IOPS, 1.31 ms |
| `fdatasync` per write, 4 KiB, QD1 | 2,318–2,438 IOPS, **0.082–0.092 ms** mean, 0.12 ms p99 | 2,970 IOPS, **0.069 ms** mean, 0.087 ms p99 |

**These are provisioned caps, not device characteristics.** The sequential figure came back as
1007.3 MiB/s and 31.790 ms on `commit7`'s `/data1`, on `commit7`'s `/data2`, and on the sidecar's `/data1`
— identical to four significant figures across two machines and three volumes, which no physical device
does. The ordering machines land on exactly half, 503.6 MiB/s. Random write sits at ~100k IOPS on the
committer machines and ~49k on the ordering machines, the same halving.

Two things follow for the figures in this document. The WAL's `fdatasync` costs **0.08 ms**, two orders of
magnitude below the 2–21 ms per-batch database commit latency these runs report, so the disk is not what
bounds commit latency — that time is spent above the device. That statement is about the *barrier* only,
and it would be wrong to read it as "durable writes are cheap here": an `O_DSYNC` write of one 4 KiB record
costs about **400 µs** on the same volume, which caps any per-record durable design near 2,400 operations a
second, three orders of magnitude below the rates measured here. It is the write that costs, not the sync
after it, and the ledger survives that only by batching — a whole block appended, a sync every hundredth.
`evaluation.tex` carries the fuller version of this in its storage table.

The two sets of numbers were taken at different block sizes and queue depths (1 MiB at QD32 and 4 KiB at
QD64 here; 1 MiB at QD4 and 8 KiB at QD32 there), which is why the sequential figure reads 1,007 MiB/s in
this table and 1,058 MiB/s in that one. Both are the same ~1 GiB/s cap approached from different depths,
not a disagreement. And at the peak of 604,545 tps the sidecar's
ledger takes about 158 MB/s of transaction bytes, 15% of one volume's sequential ceiling, so the append
path is not bandwidth-bound either at these rates.

It does put a number on §3's decay finding, and slightly reshapes it. That section attributes the
long-run decay to **disk bandwidth**, on 85–99% device utilisation at about 280 MB/s of writes per server.
Per volume that is ~140 MB/s against a measured random-write ceiling of 399 MB/s, so the volumes are busy
almost all the time while carrying about a third of their bandwidth — which points at IOPS and per-request
latency under mixed compaction traffic rather than at raw bandwidth. The direction of that finding stands;
"the storage is simply slow" is better stated as "the storage runs out of operations before it runs out of
bytes". Worth re-measuring with the fio numbers in hand before anyone quotes a bandwidth ceiling.

One caveat on the table itself: a `make start` play was transferring files elsewhere in the cluster while
these ran. The sequential and `fdatasync` figures are cap-bound and repeated exactly across volumes, so
they are solid; the random-read spread (129k on one machine, 154k on another) may carry some of that
contention and should be read as a lower bound.

## 2. Progression

Two figures matter and they are not the same. **Sustained** is the highest requested rate the
cluster actually delivered. **Peak** is the highest committed rate observed at all, usually while
badly overloaded, and is not an operating point.

| Change | Sustained | Peak | Mean latency |
|---|---|---|---|
| Starting point | 80,000 | 115,200 | 51 ms at 80,000 |
| Disable the block store's transaction ID index | 160,000 | 297,200 | — |
| Relay single-owner tracking | 160,000 | 305,600 | — |
| Load generator workers 64 → 128 | 160,000 | 325,600 | — |
| Ed25519 instead of ECDSA in the load generator | 320,000 | 374,400 | 285 ms at 320,000 |
| Spread the database front end over all 12 nodes | 320,000 | 369,200 | 251 ms at 320,000 |
| Sidecar `channel-buffer-size` 100 → 5 | — | 355,200 | **1,156 ms** (was 6,355 ms) |
| Load generator `gen-batch` 100 → 512 | — | **357,600** | 1,141 ms |

The last two rows have no sustained figure because from that point the load generator could not
offer the requested 500,000, so no requested rate was delivered. The committer committed
essentially everything offered (357,600 of 358,800), which is why the peak is meaningful there
even though the step is marked short.

Beyond that point the sequence continues, but the figures below were measured with load applied
straight to the coordinator rather than through the sidecar (section 6.1), so they are not
continuous with the table above:

| Configuration | Sustained (300 s window) | Mean latency | p99 | Coordinator RSS |
|---|---|---|---|---|
| Default manager, `waiting-txs-limit` 500,000 | 329,854 | 1,596 ms | 1,995 ms | 2.8 GB |
| Simple manager, `waiting-txs-limit` 20,000,000 | 500,258 (60 s window only) | 2,478 ms | — | 79 GB |
| Simple manager, `waiting-txs-limit` 2,000,000 | 470,815 | 4,329 ms | 4,980 ms | 8.2 GB |
| Simple manager, `waiting-txs-limit` 200,000 | 470,594 | **519 ms** | **746 ms** | 1.0 GB |
| Simple manager, `waiting-txs-limit` 500,000 | 486,941 | 1,099 ms | 1,988 ms | 786 MB |
| **Same, on a freshly wiped database** | **496,807** (99.4%, rate delivered) | **626 ms** | **968 ms** | 1.1 GB |
| Same, sustained 11 h / 17.9 billion txs | 328,433 | 1,724 ms | — | — |
| **Same, asked for 1,000,000 rather than 500,000** | **525,388 mean, 533,213 peak** | 1,050 ms | — | — |

Asking for 1,000,000 rather than 500,000 raises the ceiling to **525,388 tps mean and 533,213 peak**,
held over forty-five minutes and 2.9 billion transactions at 1,050 ms mean latency. That is the best
figure this cluster has produced. It is not the load generator being given more room in any simple
sense: offered tracks committed to within a few hundred transactions per second throughout, and
in-flight stays pinned at 530,000-570,000 against a `waiting-txs-limit` of 500,000, so the graph is
full and the generator is backpressured. The higher request simply stops the rate limiter from being
the thing that binds.

Two features of that run are worth recording because they shape how the number should be read.

**It rose into it rather than starting there.** The first twenty-five minutes averaged 482,422 tps and
then throughput stepped to 531,000 within three minutes and stayed. The trigger is not identified. Two
candidate causes were checked and both are ruled out: the table still had exactly 120 tablets, so no
splitting had occurred, and the count of running background compactions *rose* across the step (10 to
58) rather than falling, which is the opposite of a backlog clearing. Something inside YugabyteDB
settled — leader placement after the wipe is the obvious suspect — but that was not measured and is not
claimed here.

**It is a figure for a nearly empty database.** Disk sat at 17% and 67% utilisation with only 30 MB/s
of reads, against 85-99% utilisation and 134-250 MB/s of reads in the eleven-hour run above. At that
occupancy the working set is in page cache and compaction does not compete with user writes for disk.
The same run will decay as it fills, on the evidence of the eleven-hour curve.

The row before it is the best result over a 300-second ramp window and the only one where a requested
rate was delivered exactly: 500,000 offered, 496,807 committed, 99.4% efficient. It
differs from the row above it only in that the state database had just been wiped, so the table
started empty rather than holding several hundred million rows.

That difference is real and it is a decay, not noise, and over a long run it dwarfs every individual
change in this document. Left at a fixed 500,000 tps request from an empty table for eleven and a half
hours:

| Elapsed | Committed |
|---|---|
| 0 h | 479,562 |
| 2 h | **514,517** (peak) |
| 3 h | 510,800 |
| 4 h | 437,942 |
| 6 h | 408,393 |
| 8 h | 387,264 |
| 10 h | 336,317 |
| 11 h | **328,433** |

**−36% from peak**, having committed 17.88 billion transactions and filled the tablet servers to
roughly 640 GB each, about 7.7 TB across the cluster. (The row count is derived from the transaction
counter — two keys per transaction, so of order 36 billion rows in `ns_0` and 17.9 billion in
`tx_status`. Counting them directly does not finish.)

Per batch, `insert_new_key_with_value` goes from 65 ms to 133 ms and `insert_tx_status` from 52 ms to
107 ms. Both roughly double, the batch rate halves from 1,390/s to 664/s, the validator-committer pool
saturates at 192 of 192 workers, and commit-host CPU rises from 76% to 84%.

The cause is **disk bandwidth**, not anything in the committer. Every tablet server's storage is
85-99% utilised: two virtio devices each, about 280 MB/s of writes per server, two terabytes apiece and
68% full at 15.4 TB of SST across the cluster. With an empty database there is nothing to compact and
nearly all of that bandwidth carries user writes; at 15.4 TB, LSM compaction claims most of it. Write
amplification is about 13x, which is normal -- 85.7 MB/s of user data, three replicas, against roughly
3.4 GB/s of measured disk writes. The storage is simply slow and now saturated, and the committer is
waiting on it.

Measure this from `/proc/diskstats` rather than from YugabyteDB's own compaction counters. Summing
`rate()` over `rocksdb_compact_write_bytes` gives 19 GB/s and an implied amplification of 224x, which
is impossible on these disks: those counters are per tablet, tablet splitting creates and destroys the
series continuously, and `rate()` over churning series is meaningless.

The decay decelerates rather than continuing linearly — 43,000 tps lost over the first three hours
after the peak against 8,000 over the last — so there is probably a floor, but this run did not reach
it and disk sets a hard limit before it would.

The honest way to state the headline is therefore two numbers: **about 500,000 tps on a fresh database
and about 330,000 sustained after 18 billion transactions.** Quoting the first alone describes a state
the cluster occupies for two hours out of eleven.

Two consequences. Any figure quoted from this cluster has to say how full the database was when it
was taken, because a fresh table and a billion-row table differ by more than the margin between
several of the changes in this document. And a workload that only ever inserts new keys grows state
at roughly a million rows per second here, which is not a steady state at all; a workload that
updates existing keys would not.

Two further results are in that table and they are worth separating.

**The manager choice is worth 47.6%** — 486,941 against 329,854 at an otherwise identical
configuration. The row shows why: under the default manager, database batch commit falls to 62 ms
and the commit machines to 60% CPU, from 140 ms and 76%. The database is starved while the graph
sits full. Section 8 records why an earlier version of this comparison, which put the gain at 41%,
was not valid.

**The waiting limit is a latency choice, not a throughput one.** In-flight tracks whatever ceiling
the limit sets — 2,039,200 against a 2,000,000 limit — and 470,815 tps × 4.33 s reproduces that to
three digits, so above what is needed to keep the validator-committers busy the surplus is pure
queueing delay at about 4 KB of coordinator memory each. Memory tracks the limit once the limit is
what in-flight is hitting: 8.2 GB at 2,000,000 and 79 GB at 20,000,000. The 200,000 and 500,000 rows
read 1.0 GB and 786 MB, which inverts, because resident memory is a high-water mark that depends on
when the collector last ran -- at those limits the difference is inside the noise, and neither is
close to being a constraint.

500,000 is the throughput maximum and 200,000 gives up 3.5% of it for half the mean latency and a
third of the p99, so the choice between those two is a latency decision. 20,000,000 was simply a
mistake: it bought nothing, and cost 100x the memory of the setting that beats it.

## 3. Changes that raised throughput

### 3.1 Disable the block store's transaction ID index — 115,200 → 297,200

The largest single win, and the only change that moved throughput by more than a few percent.

The sidecar's block store indexed `IndexableAttrTxID`, writing one LevelDB entry **per
transaction** rather than per block. A 20-second CPU profile of a saturated sidecar attributed
**35% of its samples to goleveldb compaction**, all of it maintaining that index, which had grown
to 33 GB against 118 GB of blocks. Its snappy buffers drove a further ~17% in GC mark and ~10% in
`mallocgc`.

The signature was a throughput that decayed as the ledger grew rather than holding steady:
committed fell from 102,102 to 67,886 tps over two hours at a fixed offered rate, while bytes
written per transaction rose from 1.89 to 2.70 KB. That rise is compaction rewriting a growing
index; the transaction envelopes themselves do not change size.

Added `ledger.disable-tx-id-index` (default false). It costs `GetBlockByTxID` and `GetTxByID`, so
it suits a deployment that serves neither. The index also selects the block store's on-disk
format, so it can only be changed against an empty ledger directory.

The block number index was deliberately **not** made optional. The block store reads the last
block header through it when opening a non-empty ledger, so a sidecar without it cannot recover
from a restart — it panics with `Could not retrieve header of the last block form file: block
numbers not maintained in index`. No deployment can use such a setting, and it would save little
anyway, holding one entry per block rather than per transaction.
`TestBlockStoreReopenWithoutTxIDIndex` pins the reopen path that decides this.

### 3.2 Relay single-owner tracking — 297,200 → 305,600

Replaced the relay's two `sync.Map`s with a ring buffer of in-flight blocks and a plain map owned
solely by `preProcessBlock`. Every transaction ID is unique and short-lived, which is `sync.Map`'s
worst case: deletes leave tombstones and the dirty map is periodically copied whole.

The throughput gain was small, but the effect on the stage was not: relay status batch processing
fell from **100% to 7%** utilisation, and the wall moved on. Its own benchmark measures +18% at a
10,000-transaction block size, which is more than the cluster showed — because by then the load
generator was close to being the limit.

### 3.3 Ed25519 instead of ECDSA in the load generator — 325,600 → 374,400

Not a committer change, but the change that made the committer measurable. See
[section 5](#5-the-load-generator-became-the-limit).

### 3.4 Sidecar `channel-buffer-size` 100 → 5 — latency 6,355 ms → 1,156 ms

Throughput unchanged; in-flight fell **6×** and mean latency **5.5×**.

`channel-buffer-size` is counted in **blocks**, not transactions. At 10,000 transactions per block
the default of 100 is a million transactions per channel, and the sidecar's delivery client sizes
its own joint output channel from the same capacity
(`utils/deliverorderer/orderer.go:149` takes `max(cap(OutputBlock), cap(OutputBlockWithSourceID))`),
so roughly two hundred blocks could sit between the orderer and the relay.

That is where a plateau's 2.35M in-flight and 6.5 s mean latency came from. It was hard to find
because **every queue gauge inside the sidecar read zero** and the committer itself held only
76,000 transactions; the transactions were in unmonitored channel capacity. Block-level
accounting found them — 237 blocks submitted whose statuses had not returned, 189 not yet in the
ledger, at ~10,000 transactions each.

Anyone raising block size on this pipeline should scale this down in proportion, or the buffering
grows with it.

### 3.5 Load generator `gen-batch` 100 → 512

A local sweep of the repository's own generation path at cluster settings measured 257,104 tx/s at
100, 300,295 at 512 and 301,121 at 4096 — so 512 captures the whole gain and there is nothing
beyond it. On the cluster it was worth 344,000 → 357,600, about 4% rather than the 17% the
benchmark suggested.

The repository's tuned benchmark options already used 4096 while the deployment ran the role
default of 100.

### 3.6 Skip the tx index information no index reads — latency 1,574 → 325 ms at 482,000 tps

The largest latency win on this cluster, and it came from a stage that looked like I/O and was not.

The sidecar's ledger append runs on **one serialized goroutine**, so its cost shows up as a duty
cycle rather than as CPU on a busy machine. Ramping the mock-orderer arm, that duty cycle went 18%
→ 55% → 75% → **100%** while no machine in the cluster passed 78% CPU, and throughput stopped at
481,200 tps with mean latency jumping 249 → 1,574 ms. A request for 627,484 tps was only offered
482,400, because a full committer backpressures through the mock orderer's bounded buffer.

A CPU profile of the saturated sidecar, taken over mTLS from its monitoring endpoint, attributed the
serialized path precisely:

```
blockStore.appendBlock -> FileLedger.AppendNoSync -> blockfileMgr.addBlockInternal   0.94 cores
  blkstorage.serializeBlock                                                   92% of that path
    addDataBytesAndConstructTxIndexInfo                                       100% of serializeBlock
  blockfileWriter.append (the actual disk write)                              6%
```

So 92% of the ceiling was CPU turning a block into bytes, and 6% was writing them — consistent with
the disk sitting 4-13% busy. Separately the sidecar spent 36.5% of its CPU in GC, with `growslice`
41% of `mallocgc`.

`addDataBytesAndConstructTxIndexInfo` called `GetOrComputeTxIDFromEnvelope` — a full envelope
unmarshal — for **every transaction in every block**, and allocated a `txindexInfo` and a
`locPointer` each, regardless of what the store indexes. This deployment already runs
`disable-tx-id-index: true` (section 3.1), so every one of those txIDs was computed and thrown away.

The fix is fabric-x-common's `blkstorage/skip-unused-tx-index-info`: `serializeBlock` takes an
`indexNeeds` from `blockIndex.serializationNeeds()` and produces offsets and txIDs only when an
index will read them, and pre-sizes its buffer from `serializedBlockSize` rather than growing it.

Same day, same inventory, same Ed25519 generator, `serializeBlock` the only change:

| offered | append ms/block | append duty | mean latency |
|---|---|---|---|
| 100,000 | 18.18 → **3.37** | 18% → **3%** | 150 → **130 ms** |
| 285,610 | 19.36 → **3.44** | 55% → **10%** | 197 → **170 ms** |
| 371,293 | 20.25 → **3.53** | 75% → **13%** | 249 → **227 ms** |
| 482,680 | 20.78 → **3.65** | **100% → 18%** | **1,574 → 325 ms** |

Peak committed rose 481,200 → **509,200 tps**. Note the per-block cost is now flat at ~3.5 ms where
it used to climb with load — that climb was the per-transaction work scaling with how full each
block was.

The constraint then moved off the sidecar entirely: at the top step append was 19%, the generator
offered only 491,600 of 627,484 requested, and the busiest machine was a validator-committer at 82%
with batch commit latency 156 ms.

### 3.7 GOGC on the load generator — offered ceiling 491,600 → 552,000

Once section 3.6 removed the sidecar's serialization waste, the generator became the binding
constraint: at a requested 627,484 tps it offered only 491,600 while the sidecar's append duty sat
at 19% and its waiting queue was well below its limit.

The mock-orderer inventory had never set `loadgen_bin_env`, because the GOGC measurement that
motivated it was taken on the real-orderer arm and Ed25519 signing barely responds to GOGC. That
reasoning was too narrow — the generator builds and marshals every transaction, so its allocation
rate matters even when its signer does not.

`GOGC=400` with `GOMEMLIMIT=96GiB`: highest delivered rate 482,000 → **537,200 tps**, and the
offered ceiling 491,600 → 552,000. Latency at the new figure is 509 ms against 325 ms at 482,000,
which is the pipeline being driven closer to saturation rather than a cost of the setting.

### 3.8 Where the committer-only arm stands

Confirmed by a 300 s hold on a drained deployment, both fixes in, Ed25519 generator:

| | value |
|---|---|
| committed | **537,458 tps** |
| aborted | 0 |
| mean / p50 / p99 latency | **560 / 560 / 770 ms** |
| sidecar append duty | 20% |
| database batch commit | 190 ms |
| busiest machine | 82% |

**600,000 tps is not reachable on this hardware.** At a requested 602,112 the sidecar's waiting
queue hit its 500,000 limit and backpressured the generator to 552,000 offered, committing 548,000
— so the wall is the committer, not the generator and no longer the ledger. The database commit
path is what saturates: batch commit latency climbs 131.9 → 179.2 → 188.5 ms over the last three
steps while append stays at 19%.

Progression on this arm, all measured the same day:

| | committed | mean latency | constraint |
|---|---|---|---|
| reference | 481,200 | 1,574 ms | sidecar append, 100% duty |
| + skip unused tx index info | 482,000 | 325 ms | load generator |
| + GOGC on the generator | **537,458** | 560 ms | database commit path |

## 4. Changes that fixed a real problem but did not raise throughput

These are worth recording precisely because the reasoning behind them was sound and the outcome
still was not a throughput gain.

### 4.1 Spreading the database front end over all twelve nodes

The YugabyteDB smart driver discovers peers through `yb_servers()` and moves connections onto
them, but `github.com/yugabyte/pgx/v5 v5.7.6-yb-1` rewrites only the host, port and fallback list
on the connection config — not `TLSConfig.ServerName`. Under `sslmode=verify-full` every peer then
fails verification against the address of the endpoint first dialled, all discovered nodes are
marked unavailable, and the client silently keeps every connection on that first endpoint. An
early run put 300 of 300 sessions on one node until it answered "sorry, too many clients
already".

The first workaround pinned each validator-committer to the tablet server on its own machine with
`load-balance: false`. That was deterministic but left six of the twelve SQL front ends idle at
56% CPU while the six in use sat at 76%.

The fix is `yugabyte_tls_san_all_cluster_nodes` in the Ansible collection: name every cluster
address in every node's certificate, so the driver's stale expected name still verifies wherever
the balancer lands. Connections then spread 8–15 per node across all twelve.

The result: **database batch commit latency fell from 90–120 ms to 60–64 ms**, busiest-machine CPU
dropped 8 points, mean latency at 320,000 tps improved from 285 to 251 ms — and **throughput did
not change**. The database was never the binding constraint; it was warm because six front ends
were doing twelve nodes' work.

### 4.2 The coordinator's simple dependency graph

At one plateau the coordinator's global dependency graph looked like the constraint: its validated
batch processor sat at 95% utilisation with 40% of that spent waiting for the graph's mutex, and
the constructor at 86% with 43% lock wait, on a 64-core machine whose busiest thread was under
20%.

`SimpleManager` already existed, holding the whole waiting set in one map owned by a single
goroutine with no lock at all, and was covered by the manager tests but had no production caller.
Wiring it in behind `dependency-graph.use-simple-manager` removed the contention exactly as
designed — those stages disappear from the utilisation sweep entirely — and the committed rate did
not change, because the released pressure moved straight to the database, whose batch commit
latency rose from 90 to 120 ms.

Left defaulting to false. A lock-contention reading is a pointer to the next stage, not a
throughput gain in itself.

### 4.3 Load generator workers 64 → 128

Worth 6.5%, far less than doubling the workers suggests. The reasoning was that goroutines blocked
in a syscall hold no core, so more of them would convert idle CPU into offered load. That is true
of the scheduler but ignores that the syscall itself was the contended resource: the count of
goroutines parked in `getrandom` did not scale (38 at 64 workers, 35 at 128), and machine CPU did
not move (51.5% → 50.1%). Adding waiters to a serialized queue does not widen it.

### 4.4 Block size 500 → 10,000

No change to the ceiling. At the time the sidecar's ledger append looked like the constraint at
100% utilisation, and the hypothesis was that a fixed per-block cost would amortise over a larger
block. Append utilisation turned out to be **identical at matched throughput** — 13% at 40,000 tps
and 26% at 80,000 for both block sizes — because the cost is per transaction, not per block.
`sync-interval: 100` meant the larger blocks also cut fsync frequency twentyfold, for nothing.

The change was kept because it is harmless here and closer to the intended workload, but it bought
no throughput, and it is what made `channel-buffer-size` (section 3.4) matter so much.

### 4.5 The simple dependency graph's per-key allocations

Opened as PR **#815** (`depgraph/size-scaling`) and recorded here because the null result is the
finding. `SimpleManager.checkTXFree` built four heap objects for every key of every transaction that
found its key free — the `waiting`, its `queue` slice, a `waiterGroup`, and that group's
`[]*TransactionNode` — all with the same lifetime, all becoming garbage together when the key was
released. A workload with no contention, where a key is claimed once and released once, paid four
allocations to describe a queue of one. Holding the first group and its first member inline as fields of
`waiting` makes that case cost one. `add` is untouched: a second group's `append` sees a slice of length
and capacity one and moves the queue to the heap exactly as before, so contended ordering is unchanged.

Allocations fall by three per key, which is what the diff predicts and what `-benchmem` reports exactly
(medians over six runs of 200,000 transactions):

| shape | keys/tx | allocs/tx | bytes/tx |
|---|---|---|---|
| `rw=1` | 1 | 13 → 10 | 763 → 742 |
| `rw=4` | 4 | 30 → 18 | 1,530 → 1,440 |
| `rw=4,bw=4` | 8 | 53 → 29 | 2,525 → 2,349 |

**It saves no measurable time.** Six runs per arm, the change checked out and reverted between them, gave
+3.3% at `rw=1` and +4.2% at `rw=4` — both far inside a spread that reaches 54% — and a second machine
came out 3% the *other* way at `rw=4`. Two machines disagreeing on the sign settles it. An earlier
three-run comparison suggested 13% and was wrong for exactly this reason: six runs per arm is the minimum
this benchmark supports, and even that cannot resolve less than about 20%.

Two things bound what it is worth. `SimpleManager` **has no production caller** — `NewSimpleManager` is
referenced only by its own file and by tests, since the coordinator always constructs the global-local
manager — so on `main` the change is inert and becomes live only if the selection wiring of §3.7 lands.
And the reason to want it is memory rather than speed: this deployment's coordinator reached 79 GB of RSS
at roughly 4 KB retained per transaction, so 40% fewer allocations per transaction at four read-writes is
less for the collector to chase.

What the same work established about the graph is the more useful half, and it stands independently of the
allocation change: the graph's cost is **per key**, about 420 ns each, flat from one key per transaction
to eight. So a four-read-write transaction costs the graph four times a one-read-write transaction, and at
four the simple manager's ceiling on a single machine is around half a million transactions a second —
the same order as this cluster's 604,545 tps at one read-write and 517,273 at two. That is the mechanism
behind 9a's fall in `paper-figures.md`, and it is not the paper's: the paper attributes its own fall
across transaction sizes to lock contention in the dependency graph, while on this configuration both
lock-wait histograms have a count rate of exactly **zero**, because the simple manager takes those loops
out of the path. Per-key work produces the fall on its own.

## 4A. The orderer arm after the committer fixes, and what the ~306,000 tps wall is not

Carrying the ECDSA signer fix, the blkstorage fix and GOGC onto the real-orderer inventory:

| offered | mean latency | append ms/block | append duty | verdict |
|---|---|---|---|---|
| 150,000 | 302 ms (was 327) | 3.50 (was 18.51) | 5% (was 28%) | met |
| 187,500 | 298 ms | 3.49 | 7% | met |
| 234,375 | 305 ms | 3.43 | 8% | met |
| 292,968 | 323 ms | 3.53 | 10% | met |
| 366,210 | — | — | 11% | SHORT, only 306,000 offered |

The blkstorage win carries over unchanged. The ceiling did not move — but **its cause did**, which
matters more than the number:

- before the ECDSA fix the load generator was the busiest machine in the cluster at 77% CPU
- after it, **nothing is saturated**: routers 1018% of 3200%, batchers 486-687%, consenters 8%,
  assemblers 43%, commit machines ~60%, generator 56%

and the generator is now being *backpressured* rather than running out of CPU. What holds it back is
visible in the batchers: shard 1's mempools sit pinned at their **1,000,000 cap** on all four
replicas while shard 2's hold 9,000-57,000. That asymmetry is not routing skew — routers deliver
153,200/s to each shard and both shards cut batches at the same rate. Shard 1 simply filled during an
earlier overshoot and, running at in = out, can never drain.

### What the wall is not

At the ceiling the offered rate obeys

```
tps = decisions/s x batches/decision x batch size = 9.02 x 3.41 x 10,000 = 308,000
```

which matched the measured 308,000 exactly, and the batches were full (9,999.5 of 10,000). That made
batch size look like a free throughput multiplier, so it was raised to 20,000 (6.8 MB against the
10 MB `AbsoluteMaxBytes`). **It bought nothing:**

| | 10,000 | 20,000 |
|---|---|---|
| batches/s per batcher | 15.32 | **7.66** |
| transactions/s per shard | 153,200 | **153,200** |
| offered ceiling | 306,000 | **306,800** |
| mean latency at 300,000 | ~302 ms | **513 ms** |

The batch rate halved exactly as the size doubled, leaving the transaction rate untouched, and it
cost 210 ms of latency because a batch twice the size takes twice as long to fill. Reverted.

The value of that is what it eliminates: **the limit is per transaction, not per batch**, so it is
neither consensus batch slots nor the 100 ms decision interval. Two other candidates were ruled out
by inspection rather than by a run — the orderer does no signature work
(`ClientSignatureVerificationRequired: false`), and no component is CPU-bound.

That left flow control, and the ordering service's node-to-node egress buffer is
`SendBufferSize: 100` messages, whose own documentation says transaction messages "are waiting for
space to be freed" when it is full.

### 4A.1 `SendBufferSize` 100 -> 10,000 — 306,000 -> 336,000 offered

A real constraint, worth +10%, and the first thing all session to move that ceiling at all. Not the
whole wall though: shard 1's mempools still pinned at 1,000,000.

### 4A.2 The decision interval does not buy throughput — but it does buy latency

`requestbatchmaxinterval` 100ms -> 50ms doubled the decision rate as intended, 9.04 -> **17.04/s**,
and throughput did not follow. Transactions per decision simply halved:

| interval | decisions/s | tx/decision/shard | **tx/s/shard** |
|---|---|---|---|
| 100 ms | 9.04 | 17,200 | ~155,000 |
| 50 ms | 17.04 | 9,390 | ~160,000 |

So the conserved quantity is **per-shard transactions per second (~158,000)**, not transactions per
decision — which is also why batch size did nothing. Latency did improve, 353 -> **289 ms**, so 50 ms
is worth keeping on its own merits.

### 4A.3 Four shards instead of two — 336,000 -> 488,800

The quantity that would not move was defined *per shard*, and every batcher process was sitting at
15-20% of a 32-core machine. So the spare capacity to run another shard was already on the box:
shards 3 and 4 were added by co-locating a second batcher on each existing batcher machine, on port
7051, giving 4 parties x 4 shards = 16 batchers across the same 8 machines.

| offered | committed | mean | p99 | mempool | busiest |
|---|---|---|---|---|---|
| 340,000 | 340,000 | 334 ms | 489 ms | 157,008 | commit6 66% |
| 408,000 | 407,600 | 346 ms | 501 ms | 184,729 | loadgen 75% |
| 489,600 | 488,800 | 409 ms | 686 ms | 223,026 | loadgen 81% |
| 587,520 | 502,800 | — | — | 229,547 | loadgen 84%, SHORT |

The mempools stopped pinning — 229,547 spread over four shards at the top step against 4.1 million
pinned with two — which is the signature of the constraint having moved off the ordering service.

Confirmed by a 300 s hold on a drained deployment at 460,000: offered 459,831, committed **459,898
tps**, zero aborts, **440 ms mean / 440 ms p50 / 690 ms p99**, database batch commit 130 ms, busiest
machine 80%.

An earlier attempt at this hold was thrown away rather than reported: it was set to 489,600 straight
after the 587,520 overshoot, committed 501,898 (above its own limit, i.e. still draining) and showed
a 4.78 s p99 against a 0.60 s p50. That gap between mean and median is the tell for a backlog rather
than a rate.

### 4A.4 Where the orderer arm stands

| change | sustained | mean latency | constraint |
|---|---|---|---|
| ECDSA signer + blkstorage + GOGC | 292,800 | 323 ms | orderer egress flow control |
| + `SendBufferSize` 10,000 | 335,600 | 353 ms | per-shard throughput |
| + 50 ms decision interval | 318,800 | **289 ms** | per-shard throughput |
| + **4 shards** | 488,800 | 409 ms | load generator |
| + **quick ASN.1 digest** | **480,057** | 460 ms | **generator and database, jointly** |

### 4A.6 Replacing the reflection-built digest, and where the arm finally balances

`QuickASN1Marshal` (fabric-x-common `quick-asn1-marshal`, plus the empty-metadata fix) hand-rolls the
DER the digest is built from, replacing `encoding/asn1`. Both call sites use it — `utils/signature`'s
verifier and `utils/testsig`'s signer — so the change lands on the committer's verification path as
well as the generator's signing path. At the transaction shape this cluster generates it is 5x cheaper
(7,510 -> 1,512 ns/op, 61 -> 9 allocations).

Verified by drained 300 s holds:

| | before | after |
|---|---|---|
| hold at 460,000 | 460,136 tps, 390 ms, 600 ms p99 | **460,000 tps, 380 ms, 580 ms p99** |
| hold at 480,000 | — | **480,057 tps, 460 ms, 700 ms p99** |
| offered ceiling | ~503,000 | ~496,400 |
| busiest machine at the ceiling | loadgen | **commit6, with loadgen level** |

Zero aborts throughout, which is the load-bearing check: the verifier recomputes the digest
independently of the signer, so a single byte of disagreement would fail every transaction.

The ceiling itself did not move, and that is the result. What changed is that the generator stopped
being the sole constraint — at 496,400 tps the machines sit at:

```
commit machines (database + validator-committer)  73.8 - 79.4%
load generator                                    78.4%
verifiers                                         40%
coordinator                                       18.8%
sidecar                                            8.3%
```

The generator and the database arrived at the same utilisation together, so neither alone is the
bottleneck any more and freeing one buys nothing without the other. Database batch commit latency
climbing 110.7 -> 169.6 ms over the last two steps is the database's half of that.

One measurement note. The first 300 s hold at 480,000 reported a 4.48 s p99 against a 460 ms p50; a
fresh window on the same unchanged hold read 700 ms. The gap between mean and median is the tell — the
five-minute window still contained the ramp-up transient. In-flight was flat at 203,704 both times,
which is what says the rate itself was steady.

**1.67x on this arm**, and the ordering service is no longer the bottleneck: at the top step nothing
in it is saturated (mempools unpinned, sidecar wait below its limit, ledger append 19%) while the
load generator sits at 84% CPU and cannot offer more than ~503,000 tps.

The committer's own wall, measured independently on the mock-orderer arm, is ~548,000 tps at the
database commit path — so for the first time the two limits are within 10% of each other, and the
next real gain needs either a second load generator (legitimate here: the one-generator rule is a
mock-orderer artifact) or the database.

### 4A.5 A one-hour soak: no decay

Peak measurements cannot see drift, and this pipeline has drifted before — section 3.1's txID
index took committed throughput from 102,102 to 67,886 tps over two hours at a fixed offered rate
as its LevelDB compaction grew with the ledger. The new append path touches the same subsystem, so
it is worth showing it does not reintroduce that.

One hour at 200,000 tps on the four-shard configuration, drained start:

| | t+20m | t+40m | t+60m |
|---|---|---|---|
| committed | 200,000 | 200,000 | 200,034 |
| mean latency | 320 ms | 330 ms | 340 ms |
| database batch commit | 40 ms | 40 ms | 40 ms |
| aborted | 0 | 0 | 0 |

Flat, with p99 at 500 ms and 1.098 billion transactions committed cumulatively. Nothing down.

The soak length is bounded by disk, not by stability. Measured by `deriv` on the filesystem
gauges, the sidecar and **each of the four assemblers** independently write 68.7 MB/s at 200,000
tps — 344 bytes per transaction each, matching the envelope size, but **five copies cluster-wide**,
about 1.7 KB of disk per transaction. The assemblers' 485 GB volumes therefore reach the watchdog's
80 GB floor long before the sidecar's 968 GB does: roughly 70 minutes at 200,000 tps, or 1 hour 25
minutes at 280,000 as section "Ninety minutes of disk" records.

## 5. The load generator became the limit

From roughly 325,000 tps onward, most measurements were of the harness rather than the committer.
This is the single most important caveat on every number in this document.

**ECDSA signing.** Switching the namespace policy from `ECDSA` to `EDDSA` took the peak from
325,600 to 374,400 tps. That result stands. The mechanism first recorded here was wrong, and the
correction matters because it changes what to do next.

The original account was that Go's ECDSA draws a nonce per signature through `getrandom(2)`, for
which this kernel has no vDSO, and that the syscall was the limiter — a goroutine dump had 38 of 95
goroutines parked in an identical stack ending in `syscall.Syscall` while the machine sat at 51% CPU.
The dump was real; the attribution was not. Two measurements refute it:

- Replacing `rand.Reader` with a userspace ChaCha8 CSPRNG in the ECDSA path changes nothing:
  6,615 against 6,687 ns/op at 16 procs. If entropy were the limiter, removing the syscall would
  have moved it.
- `GOGC=off` gives **5.2×** on 32-proc ECDSA signing (11,613 → 2,215 ns/op) while Ed25519 barely
  moves (1,412 → 1,278). **Garbage collection was the limiter**, and the reason Ed25519 wins is that
  it allocates 184 B and 4 objects per signature against ECDSA's 6,067 B and 59 — a factor of 33 in
  allocation, on a machine whose throughput tracks allocation rate.

`getrandom` does stop scaling past about 16 threads, but its ceiling is ~12M reads/s, orders of
magnitude above the rates here.

Ed25519 signing does not read entropy at all — RFC 8032 derives the nonce from the key and message,
and Go's `ed25519.Sign` ignores its `rand` argument — so the claim that Ed25519 *forces* `getrandom`
is doubly wrong. Verified on the cluster's own 64-core load generator host under load at ~570,000 tps:
a goroutine dump held 128 signing workers with 21 inside `ed25519.SignCtx` and **zero** frames
matching `GetRandom`, `sysrand` or `drbg`. The blocked-goroutine stacks in the original dump were
therefore something else on the ECDSA path, most likely the MSP envelope signature (BCCSP signs with
`ecdsa.Sign(rand.Reader, ...)`), which is active only when `policy.identity` is set — it is not set
in this deployment.

One trap: the load generator's policy template pinned `key-path` at the Fabric CA's MSP signing
key, which is ECDSA P-256. The endorser accepts a mismatched key at construction and only fails on
the first signature, with `ed25519: bad private key length: 227` — so the deployment starts cleanly
and dies under load. The collection now omits `key-path` for any non-ECDSA scheme.

**What still limits it.** The generator is signing-bound and parallel: 76% of its CPU is in
`TxEndorser.Endorse`, 59% in Ed25519 `SignCtx` alone, working out to ~60 µs of CPU per transaction.
The repository's `BenchmarkGenTx` shows it scaling with core count until workers ≈ cores and then
flattening. At 358,800 tps it uses about 21 of 64 cores, so it is not machine-CPU-bound, and
neither deeper channels (`buffers-size` 100 → 2000, no change) nor more workers moved it much.

The committer's own ceiling is therefore **not known** above roughly 370,000 tps. Measuring further
needs a faster generator — a second generator machine, or less signing work per transaction, which
would change what is being measured.

**Block preparation, the second generator limit.** Signing is not the only place the harness caps the
measurement, and the second one is what made the small-block figures the generator's rather than the
committer's. The sidecar adapter cuts its own blocks and hands each to an embedded mock orderer, whose
single goroutine calls `testcrypto.PrepareBlockHeaderAndMetadata` before serving it. That call
deep-clones the block and hashes all of its data, and both costs scale with the block. Benchmarked on
300-byte transactions:

| per block | 500 tx | 10,000 tx |
|---|---|---|
| `proto.CloneOf` | 0.16 ms | 3.1 ms |
| `ComputeBlockDataHash` | 0.54 ms | 11.1 ms |
| number, chain, sign, marshal metadata | 0.05 ms | 0.05 ms |

Fitting the two cluster measurements — 853 blocks/s at 500 transactions, 60.5 blocks/s at 10,000 — gives
1.62 µs per transaction and 0.36 ms fixed per block, and the 1.62 µs agrees with the 1.68 µs the clone
and the hash cost together in the benchmark. That is the whole small-block story: the same per-block
cost spread over a twentieth as many transactions.

Neither cost needs to be there. The clone protects a caller who reuses a block, and this producer builds
one per call; the hash covers the block's own data, so unlike the number and the previous hash it does
not depend on the chain and can be computed a stage earlier. With `fast-block-prepare` the adapter
hashes in its mapper goroutine — which was nearly idle, since transactions arrive already serialized —
and preparation falls to **8.4 µs at 500 transactions and 8.3 µs at 10,000**, independent of block size.

Two cautions. The flag is off by default, so no figure already taken is silently compared against a
generator that behaves differently; and the expected cluster gain is about **2×, not the 72× the stage
benchmark shows**, because preparation was only about half the generator's per-block budget and the
mapper now carries the hash, which makes it the next limit near 1,850 blocks/s. Past that the hash needs
an ordered pool rather than one goroutine.

This also cost three false diagnoses worth recording. A first benchmark reported 537 ns/block, which was
the channel write to the preparing goroutine and not the preparation behind it — `SubmitBlock` only hands
the block over, so the buffer between submitter and preparer has to be pinned to two blocks for the
number to mean anything. A first attribution blamed the mock orderer's per-envelope dedup cache, which
SHA-256s and base64-encodes every payload; that path is real, and expensive, but this adapter never uses
it, because it submits whole blocks. And three runs were killed on the belief that the fast path
deadlocked, when what was slow was per-case crypto generation in the benchmark's own setup.

## 5C. The ordering arm is four shards, and what its second disk is now for

Two facts about that arm, both verified against `cluster-orderer.yaml` and the machines rather than
inherited from the plan or the paper:

**It is four parties by four shards, not four by two.** Sixteen batchers, `batcher{party}-{shard}`, share
eight machines two apiece on ports 7050 and 7051 with operations on 7060 and 7061; routers, consenters and
assemblers get a machine each. The pairing has a measured justification recorded in the inventory: at two
shards the service capped near 158,000 tps **per shard** whatever the batch size or decision interval,
while batcher processes sat at 15-20% of a 32-core machine, so the constraint is per shard and the spare
capacity for a second one was already on the box.

This changes which published number the arm is compared against. Figure 7a at four parties reads 280,000
tps at one shard, 414,000 at two and **430,000 at four**, so 430,000 is the ordering ceiling for this
topology, not 414,000. It also means the published size sweep (Figure 7b) is a *two-shard* measurement: its
shape is comparable, its absolute values are not.

**Each batcher machine has two 484 GB disks, and now each co-located batcher gets one.** They both used
`/data1` originally -- `batcher1-1` and `batcher1-3` side by side while `/data2` sat mounted and empty -- so
if the point of pairing shards on a machine was a spindle each, the deployment was not doing it. That
confounds exactly the shard-scaling question the arm exists to answer, so `orderer_data_dir` now names
`/data2` for the second batcher on each machine. No collection change was needed: `orderer_data_dir`
resolves per *inventory host*, and each batcher process is its own inventory host sharing an
`ansible_host`.

That produces the two configurations now being compared, which separate the two things a shard needs -- a
core budget and a disk:

| | shards | batchers | per machine | volume |
|---|---|---|---|---|
| `cluster-orderer.yaml` | 4 | 16 | 2 | one each |
| `cluster-orderer-8shard.yaml` | 8 | 32 | 4 | two share one |

(Both devices report `rotational=1` and are virtio-backed, so they are not NVMe whatever the provisioning
notes say.)

## 5A. Switching arms: three faults that all look identical

Recorded because five bring-up attempts were spent on it, and because each fault produces the *same*
assembler panic --- so fixing one and retrying looks like no progress at all:

```
setting up the MSP manager failed: the supplied identity is not valid:
x509: certificate signed by unknown authority ... candidate authority certificate "fca-org1"
```

- **`make teardown` does not remove a host's MSP.** The committer sidecar's certificate from the previous
  arm survived every teardown. `make setup` fetches each host's existing MSP into the org tree, so that
  stale leaf landed in org1's `msp/knowncerts`, the genesis block embedded it, and the assembler rejected
  the bundle. Fabric validates **every** certificate in an MSP, so one stale leaf is fatal while the
  freshly enrolled ones beside it verify perfectly.
- **Wiping only the CA re-initialises its key.** `make hard-wipe TARGET_HOSTS=fabric_cas`, carried over
  from the committer arm's stale-admin-MSP problem, gives a new CA key while hosts keep identities
  enrolled under the old one --- measured as a cacert and a leaf 92 seconds apart, both named `fca-org1`,
  that fail `openssl verify` against each other.
- **Teardown without a CA wipe fails a third way.** Teardown clears the CA's registry, which lives on its
  own database host, while the admin MSP on the CA host survives, so the next enrolment gets
  `Code:20 Authentication failure`.

The recipe that satisfies all three is `make hard-wipe TARGET_HOSTS=all` **and** removing the CA's
containers and directories on the control node, in the same pass. Neither alone is enough, and this took
several attempts to see because each one fixes half the problem: the wipe play does not clean the control
node, where the CA lives, so its key and admin MSP survive it; and removing only the CA leaves every
worker holding an identity the new key never signed. The gate below caught both halves, twice, naming the
four surviving certificates each time.

One trap inside the trap: the CA database's directory is owned by a container-mapped uid, so a plain
`rm -rf` fails with `Permission denied: .../pgdata` and *silently* leaves the old registry behind --- one
attempt ran with a CA key from 07:45 and a registry from 07:27 as a result. It needs
`podman unshare rm -rf`.

A per-point redeploy on this arm is therefore not just slow but self-defeating: every `teardown` breaks the
CA and costs a full reset to recover. That is what `FX_DEPLOY_PLAN=none` exists for --- one deployment for
a whole ladder, with the bias that consecutive rungs share it.

**And a method note that cost more than the faults.** The first gate written to catch this sampled one
certificate with `find ... | head -1`, happened to pick a freshly enrolled user cert, passed, and let a
deployment proceed that could not start. When the fault is one bad member of a set, sampling the set is
not verification --- the gate now checks every certificate in every org tree. This is the same lesson as
"verify the artifact, not the exit code", one level down: verify the *whole* artifact.

## 5D. Why the end-to-end arm never committed, twice over

The arm produced no valid measurement at all until 2026-09-09, across roughly a dozen bring-ups. Two
distinct faults, and the reason it took so long is that **both present identically**: the whole pipeline
runs, Arma cuts blocks, the sidecar delivers them, the committer validates them, block height climbs, and
100% of transactions come back `ABORTED_SIGNATURE_INVALID`.

### The wipe was never wiping the database

`make hard-wipe` clears `/data1/fabric-x`. YugabyteDB's data directories are

```
yb-master   --fs_data_dirs=/data1/yb-master
yb-tserver  --fs_data_dirs=/data1/yb-tserver,/data2/yb-tserver
```

which are **siblings** of `/data1/fabric-x`, not children of it. So every "clean" bring-up ran on a
database that predated the crypto reissued minutes earlier, keeping a namespace policy signed by a retired
key. That is the 880,706 `ABORTED_SIGNATURE_INVALID` against exactly 1 `COMMITTED` recorded in section 5A,
with the pipeline entirely healthy --- healthy because it was.

It also hangs YugabyteDB's own init script. `01-yb-init.sql` opens with `create database yugabyte`, which
should fail instantly against a database that already exists; instead it sat 22 minutes in
`RPCWait/CatalogRead` while the cluster reported three masters with a leader, twelve tservers `ALIVE` and
sub-second heartbeats. A stale catalog under re-keyed masters does not error, it waits.

Three of my own diagnoses of that hang were wrong and are retracted: that two of every ten `tx_status`
tablets were leaderless and needed an election nudge; that the 120-way tablet pre-split was too expensive
(a fresh 120-tablet table creates in 336 ms); and that raising `committer_db_init_timeout` to 20m would
cover it (the 20m run failed after 15:33 with the same once-a-minute pattern). Pre-split is back to 120 and
`init-db` completes in **4 minutes**. The lesson is narrower than any of those theories: read the data
directories off the running process, do not infer them from the deployment directory.

Worse than missing both paths was catching one. An intermediate version of the wipe removed
`/data2/yb-tserver` only, which leaves each tablet server with one live data directory and one deleted ---
corrupt rather than merely stale. The wipe now covers all three paths and **gates** on them: `ls -d
/data1/yb-master /data1/yb-tserver /data2/yb-tserver` must come back empty on every host before `setup`
runs.

### The namespace has to be created by `make init`

`loadgen_generate_namespace` is `false` on this arm, deliberately: a namespace-creation transaction writes
to the `_meta` namespace, whose policy is an MSP rule, and the loadgen role renders `artifacts-path` only
when the mock orderer is in use. Without it the generator builds a `_meta` endorser with no identities and
the transaction arrives carrying zero signatures (`MALFORMED_MISSING_SIGNATURE`). So namespace creation
belongs to `make init`, where `fxconfig` submits the envelopes through a router signed with the Fabric CA
identity it enrolled --- and the bring-up script simply never ran it.

`make init` reports every non-loadgen host as `skipped` and exits 0 regardless; the real work runs on the
loadgen host alone. Read the namespace list, not the exit code.

The working order for this arm is therefore:

```
stop -> wipe (both volumes AND all three yb paths) -> setup -> gate on crypto
     -> start -> init -> gate on a committed rate
```

Sixty seconds after `make init` the arm committed full blocks (`COMMITTED x 256`) and the first ladder
began.

## 5B. What the disks are, and what they bound

Every node that writes has the same shape, and it is worth stating because two of the numbers below bound
results elsewhere in this document.

**Configuration.** Two data disks per node, XFS with `noatime,nofail`, mounted `/data1` and `/data2`:
969 GB each on the nineteen committer machines, 485 GB each on the twenty ordering machines. They are
`virtio` block devices, so they report no model string and their rotational flag is meaningless — the
backing media cannot be identified from inside the guest, only measured. The I/O scheduler is `none` on
every device.

**Use differs by role, and an early version of this section got it wrong.** Each database node gives
YugabyteDB *both* volumes as separate data directories rather than a stripe, so it sees the disk boundary:
`/data2/yb-tserver` exists on every tablet host. The sidecar's ledger and every ordering component — router,
batcher, consenter, assembler — write to `/data1` alone and leave the second volume idle, and on the batcher
machines two batchers share a host, one shard each, both on the first volume.

The correction is worth recording because of how the error was made: `/data2` was observed empty on three
nodes and written up as "unused on every node", but the observation was taken minutes after
`hard-wipe TARGET_HOSTS=all`, when no database had started yet. An empty data directory on a wiped cluster
is evidence of nothing. The database nodes — the heaviest disk consumers here — do use both.

**Measured with `fio` 3.35**, `libaio`, `direct=1`, 20 s per pattern, run on the *unused* second disk of
each class so nothing live was touched:

| pattern | depth | committer class | ordering class |
|---|---|---|---|
| 1 MiB sequential write | 4 | 1,058 MiB/s, p50 4.1 ms | 529 MiB/s, p50 8.2 ms |
| 4 KiB write, `O_DSYNC` | 1 | 2,336–2,474 IOPS, p50 395 µs | 3,249 IOPS, p50 297 µs |
| 8 KiB random read | 32 | 121,628–131,575 IOPS, p50 ~240 µs | 67,689 IOPS, p50 489 µs |
| 8 KiB random write | 32 | ~102,000 IOPS, p50 315 µs | 51,165 IOPS, p50 651 µs |

Two conclusions, and one methodological trap.

- **These are rate-capped volumes, not devices.** Every bandwidth row differs between the classes by almost
  exactly a factor of two, at round numbers — 1,057.6 against 528.8 MiB/s. A cap explains that; two
  generations of physical media would not land on 2.000. So the ordering machines have half the disk
  bandwidth on top of half the cores and half the memory, and any ceiling measured on that arm has to be
  read against the cap before it is attributed to ordering. The synchronous row is the exception that
  proves the point: there the ordering disks are *faster*, 297 µs against 395 µs, because a cap on
  bandwidth does not bind a latency-bound single-queue write.
- **A per-record `fsync` path could never have worked here.** Making one 4 KiB record durable with an
  `O_DSYNC` write costs about 400 µs. The barrier alone is far cheaper — an `fdatasync` after a buffered
  write returns in about 0.08 ms, measured separately — so it is the write and not the sync that costs, and
  a figure quoted for "synchronous write" has to say which of the two it means. Any design that syncs once
  per transaction is therefore capped near 2,400 a second — three orders of magnitude
  below the rates in this document. The ledger only survives because it batches: it appends a whole block
  and syncs every hundredth (`sync-interval: 100`), which is why §3 could measure the append path at
  0.23 ms per 10,000-transaction block rather than at 400 µs per transaction.
- **The trap:** fio's default `psync` engine silently caps the queue depth at 1 while still printing the
  depth that was asked for. A first run reported "iodepth=32" figures that were depth-1 measurements. The
  deep rows above use `libaio`; a `note:` line on stdout is the only warning fio gives, and it also breaks
  JSON output parsing.

## 6. Where the constraint is now

Signature verification, as of section 6.3. It was the database commit path, at roughly 487,000 tps,
and the rest of this section describes it as such because that is how it was found; raising the
validator-committers' commit workers moved it. The description below still holds for the commit
stage at 32 workers per validator-committer. With the default graph manager the constraint is the coordinator and the database
idles behind it (section 4.2); with the simple manager the database becomes the constraint and the
figure below is what that looks like. Of the validator-committer stages,
`vcservice_database_tx_batch_commit` runs about 191 workers concurrently busy and
`..._insert_new_key_with_value` about 90, batch commit latency is 140 ms, and the six commit
machines sit at 74-76% CPU. Every coordinator queue is empty, which places the constraint below
the coordinator rather than in it.

### 6.1 Load applied straight to the coordinator

From this point the load generator submits to the coordinator directly through `CoordinatorAdapter`,
with the sidecar stopped, rather than serving blocks to the sidecar. The reason was that the
committer and the generator had come within a few percent of each other, so an end-to-end
measurement reports the slower of the two and cannot say which. Taking the sidecar out settled it:
355,995 tps coordinator-direct against 357,600 through the full pipeline — the sidecar was never
the constraint, and its removal bought nothing.

Two things to know before reproducing this. The coordinator's `BlockProcessing` stream is exclusive
(`TryLock` in `coordinator.go`), so the sidecar must be stopped, not merely bypassed. And the
coordinator needs the namespace's configuration transaction, which normally arrives through the
sidecar; without it every transaction returns `ABORTED_SIGNATURE_INVALID`.

### 6.2 Putting the sidecar back, and what it costs

Nothing, once the two in-flight windows are the same size. Measured at the same request of
1,000,000 tps that produced the best coordinator-direct figure, and over the same length of run:

| | 45-minute mean | Peak | Mean latency |
|---|---:|---:|---:|
| Coordinator-direct, coordinator window 500,000 | 525,388 | 533,213 | 1,050 ms |
| Full pipeline through the sidecar, both windows 500,000 | **523,316** | **548,800** | 5,400 ms |

The means differ by 0.4%, which is inside this cluster's run-to-run spread, and the peak is 2.9%
higher through the sidecar. The latency is five times higher, which is what an extra stage plus a
deeper buffer in front of it is supposed to cost. Throughput is not.

Two mistakes are worth recording because the first one produced a plausible wrong answer.

**Do not let the sidecar's `waiting-txs-limit` undercut the coordinator's `dep-graph-wait-tx-limit`.**
Every figure in the table above the sidecar was reintroduced into was taken with the coordinator's
500,000 as the pipeline's binding in-flight window. Set the sidecar's limit to 300,000 and the
sidecar's window silently becomes the tighter of the two and replaces it. Since throughput is
in-flight over latency, that caps throughput and not merely latency — the earlier sweep in this
section prices it directly at 470,594 tps for a 200,000 window against 486,941 for 500,000, and the
first sidecar-in-path measurement, at 300,000, returned 480,689: on that curve, at that window. It
was briefly reported as a 10% cost of having a sidecar. It was the cost of a window.

**A long-run mean is not a rate the cluster held.** The 525,388 above spans an unexplained step up:
its own first twenty-five minutes averaged 482,422 tps before throughput jumped to 531,000 within
three minutes. The sidecar run did not reproduce that step and instead decayed monotonically from
542,080 tps over its first five minutes to 512,080 by minute forty-five, which is the ordinary
state-growth decay documented in section 2. So the two 45-minute means agreeing to 0.4% is partly
coincidence; the early-window comparison, 542,080 against 482,422, favours the sidecar run, and
neither comparison supports a throughput cost.

### 6.3 Committer workers 32 to 64, and where the constraint went next

The commit stage was running at 99.4% worker occupancy while the machines it runs on were
not close to saturated, which is a concurrency limit and not a resource one:

| | 32 workers/VC | 64 workers/VC |
|---|---:|---:|
| concurrent database commits | 190.9 of 192 | 228.4 of 384 |
| transactions per batch | 331.6 | 354.5 |
| commit latency | 139.3 ms | 159.6 ms |
| predicted tps (concurrency ÷ latency × batch) | 454,500 | 507,400 |
| measured tps | 454,394 | 507,537 |
| commit-machine CPU / disk | 74% / 44% | 78% / 45% |

Worth **+6.8%** on a same-day comparison, 480,000 to 512,400 over matched twelve-minute
windows, and the resulting run was unusually flat: 512,160 tps over its first five minutes and
509,067 over minutes 25-30, against the visible decay every earlier run showed. The connection
pool had to grow with it (64 to 128), because 64 committer workers would otherwise consume the
whole 64-connection pool and starve the preparer's version reads and the validator.

This is not the change section 4.3 rejected. That one raised worker counts while every
connection was pinned to a single tablet server, so the extra workers queued harder on one
node; connections now spread across all twelve, and the workers were measurably saturated
first. Neither was true then.

**The constraint moved to signature verification.** At 64 workers the commit stage runs at 59%
occupancy and every queue in the pipeline is empty except one:
`coordinator_verifier_input_batch_queue_size`, at 58. The verifier machines sit at 34% CPU, so
they are not compute-bound either — the same shape as the commit stage before this change. The
likely mechanism is in their batching: `batch-size-cutoff` is 500 and `batch-time-cutoff` is
2 ms, and at 169,493 tps per verifier a 500-transaction batch takes 2.95 ms to fill. Every
batch is therefore cut by time at roughly 340 transactions, and split across `parallelism: 128`
goroutines that is under three signatures per goroutine per dispatch — coordination rather than
verification, which is what 34% CPU at 508,478 tps verified looks like. Untested; raising
`batch-time-cutoff` past the fill time is the experiment.

### 6.4 The cluster's baseline moves between days, so an A/B has to be same-day

Identical code and configuration measured 537,567 tps one day and 454,408 the next, on fresh
deployments both times — about 15% apart, with the database disks 90% empty in both cases and
Raft leaders evenly balanced at 71-73 per tablet server. Nothing in this repository accounts
for it, and it is larger than most of the individual changes in this document.

The practical consequence is that a figure from yesterday is not a control for a figure from
today. This cost a wrong conclusion: after rebasing onto the Go 1.27 upgrade, the pipeline
measured 472,400 against the previous day's 529,471, and the isolation looked airtight — the
only upstream commit the rebase introduced was the toolchain bump, and `go version -m` on the
two binaries showed identical dependency sets, 88 deps with no differences, so every other
candidate was excluded. It was reported as a 10.8% Go 1.27 regression. Rebuilding the
pre-upgrade binaries from a safety tag and running them on the same cluster the same day
returned 454,408 — *slower* than the rebased build. There was no regression; the baseline had
moved.

So: tag the tip before any change that could plausibly affect throughput, and when a
measurement moves, rebuild the old artifact and measure it today rather than reasoning about
what changed. A clean account of *what* differs is not evidence that it *caused* anything.

### 6.5 The sidecar's ledger bounds how long a run can last

There is no retention or pruning in the sidecar's `LedgerConfig` — the ledger is append-only.
At roughly 334 bytes per transaction it writes about 170 MB/s at 500,000 tps, which fills the
sidecar's 969 GB `/data1` in around 1.6 hours. A run that exceeds that ends with
`error appending block to file: ... no space left on device`; the sidecar exits on it rather
than corrupting the ledger, which is the right behaviour but does end the run.

That is why the eleven-hour decay curve in section 2 was measured with load applied straight to
the coordinator. Reproducing it end-to-end through the sidecar would need roughly 6 TB of
ledger, which this hardware does not have on one mount.

### 6.6 What the load generator can offer

Not the constraint any more, but close enough to matter. One 64-core generator benchmarks at
598,208 tx/s on the submit path (generation, block mapping, TX ID extraction, metrics and latency
hooks, with a sender that does nothing) and 600,837 tx/s with a block marshal added, so gRPC and
the status-receive path are what separate that from the 487,000 it offers in the deployment.

A short-offer reading — the generator offering less than the requested rate — has two possible
causes and a ramp cannot distinguish them. There is one case where it can: the generator offered a
full 500,000 tps at the 500,000 step and only 447,600 at the 550,000 step. A generator ceiling is a
constant and cannot fall when more is asked of it, so that drop is committer backpressure.

Constraints found and resolved, in order:

| Constraint | Resolution |
|---|---|
| Sidecar block store txID index compaction | removed (`disable-tx-id-index`) |
| Relay `sync.Map` churn | removed (single-owner tracking) |
| Load generator ECDSA allocation rate | removed (Ed25519, 33x fewer bytes per signature) |
| Coordinator dependency graph mutex | removable, no throughput gain |
| Database SQL front-end concentration | fixed, no throughput gain |
| Sidecar channel buffering (latency only) | reduced 6× |
| Load generator signing throughput | removed (Ed25519, `gen-batch`) |
| Sidecar block delivery | shown never to have been the constraint |
| Sidecar block store serializing unused txID index info | removed (`fabric-x-common` `serializeBlock`) |
| Sidecar `waiting-txs-limit` below the coordinator's window | matched to it; was misread as a sidecar cost |
| Coordinator dependency graph halting under load | fixed (`drain_test.go`) |
| Oversized `waiting-txs-limit` (latency and memory) | 20M → 500K, 100× less memory |
| Database commit worker count | 32 → 64 per VC, +6.8% |
| **Signature verification batch cutoff** | **current, untested** |

## 7. How the constraint was located each time

The methods that worked, and the readings that misled.

**Utilisation and queue depth have to be read together.** A stage at high utilisation whose
*input queue is empty* is starved, not limiting. This distinction resolved three
misattributions: relay block mapping read 82–93% with an empty input queue while the real limit
was upstream, and ledger append read 100% while its input queue never backed up. Conversely a
**full** queue sits immediately downstream of the constraint — the coordinator's VC output queue
pinned at 60/60 correctly identified the validator-committers, and its status output queue pinned
at 60/60 correctly identified the relay. `bin/fx-diagnose.py` in the evaluation harness prints
utilisation, queue depth against capacity, and host CPU together for this reason.

**Machine CPU cannot find a serialized stage.** On 64 cores a single saturated goroutine reads as
about 2% of the machine. Every serial constraint here was found with per-thread CPU (`top -H`), a
goroutine dump, or a CPU profile — never from a host metric.

**A goroutine dump says where work waits; a CPU profile says where it goes.** Both were needed and
each alone was misleading. The dump found the blocked signing goroutines that no CPU measurement would
have shown. But when 128 workers appeared blocked on a channel write, the dump suggested a slow
consumer, and the profile showed the cost was entirely parallel signing — the channel was merely
momentarily full.

**Aggregate CPU share does not identify a serial bottleneck.** "76% of CPU is signing" is true and
was measured across 128 parallel goroutines; it says nothing about which single-threaded stage sets
the rate. Utilisation of an individual stage does.

**Beware apparent saturation that is really feedback.** The ledger append read 100% utilised with
an empty input queue because `AppendNoSync` writes through the page cache, and dirty-page writeback
throttling stretches each write to absorb whatever slack exists. It self-adjusts to the arrival
rate, so its utilisation was a consequence of the load, not a cause.

**Bandwidth that is flat is not necessarily saturated.** The sidecar's disk sat at exactly
185 MB/s while throughput decayed, which looks like a hard cap. It was not: the same volume
sustained 1.1 GB/s under `O_DIRECT` while the sidecar was using it. The flatness was the workload's
demand, and the decay was bytes-per-transaction rising as an index grew.

**Verify the artifact, not the deploy's exit status.** Several changes deployed with zero failures
while not being live: a binary that was not rebuilt, a branch that was never merged, crypto
material that `teardown` preserves and only `wipe` reissues, the wrong one of two crypto code
paths, and a task whose `when` guard was silently false. Ansible does not display skipped tasks, so
grepping a deploy log cannot distinguish "not in the file" from "skipped". Check the rendered
config on the target host, the deployed binary's size, the certificate's SANs, or a log line
proving the new path was taken.

**Test harness hypotheses locally.** The `gen-batch` sweep in section 3.5 took a 29-second
benchmark against the repository's own generation path, rather than a 15-minute cluster cycle, and
gave a sharper answer than the cluster could.

## 8. Measuring without fooling yourself

Four ways the measurements in this document were wrong before they were right. Each cost a figure
that had already been written down.

**A 60-second window catches transients a 5-minute window does not sustain.** 500,000 tps requested
committed 499,356 over 60 s and 486,941 over 300 s from a clean deployment — and 462,296 over 300 s
in a run that had already been pushed past the knee. All three are the same build at the same
requested rate. Quote the 300-second figure from a fresh deployment; use short windows only to
locate a knee.

**Overload does not drain, so it contaminates everything after it.** The graph's slots are released
only as the validator-committers return results, so a backlog can drain no faster than the committed
rate. Recovering a quarter of a million queued transactions takes minutes, and `fx-ramp.py`'s 60-second
settle does not cover it. Any step following an overloaded one reads low. This is why the apparent
"collapse" past the knee — 550,000 requested delivering less than 500,000 requested did — is partly
hysteresis and not purely a throughput cliff.

**Two measurements taken at different times are not a comparison.** The simple dependency graph
manager committed 500,258 tps where the default manager had committed 355,995, which looks like a
41% gain and is not one: the default manager's figure was recorded while the load generator was
itself the limit at about 358,000 tps, so it is a floor rather than that manager's ceiling, and the
later run came after the generator had been made faster. The gain credited to the manager includes
the generator's. The defensible comparison is the in-repository benchmark, where both managers run in
the same harness over the same transaction count with no generator involved: 321,899 against 249,691
tx/s, 29%.

**"The two dependency graph managers track different dependencies" was asserted and is false.** The
issue draft for the simple manager said the default one tracks dependencies the simple one does not,
so selecting it was a semantic trade-off. Reading both implementations refutes it. They derive their
keys from the same `readAndWriteKeys`, and they encode the same coarse-grained relation -- two
transactions sharing a key conflict unless both only read it -- one as `getDependenciesOf` copying the
write maps for a read key and all three for a write key, the other as `waiting.add` letting a reader
join a running group of readers and queueing anything else. `TestDependencyGraphManager` already
asserts identical behaviour against both, which is evidence the repository never intended a
difference. What differs is the data structure and the concurrency, which is what the 47.6% comes
from.

Re-validating it did find one real divergence, and it is a defect in the simple manager rather than
extra tracking in the default: it can make a transaction wait on itself. `readAndWriteKeys` adds
`_meta:<ns>` as a reads-only key for each non-system namespace, so a transaction that both updates a
namespace's policy through `_meta` and writes inside that namespace carries the same composite key as
a reads-and-writes key too. `processTxBatch` calls `checkTXFree` for it twice, and the second call
queues the transaction behind the running group the first call created. Confirmed by driving one such
transaction through both managers: the default releases it, the simple releases nothing, and
per-namespace duplicate-key validation does not catch it because the two contributions come from
different namespaces. The lesson is the ordinary one -- the claim was made from the measurement rather
than from the code, and reading the code was what settled it, in both directions.

**One parked goroutine was written up as a benchmark that could not run at all.**
`BenchmarkDependencyGraph` numbered its batches from 0, and the local dependency constructor releases
a batch only after its predecessor, so batch 0 does `CompareAndSwap(0-1, 0)` against a counter that
starts at 0 and can never succeed. That much is real. The conclusion drawn from it — that every
default-manager case hung and the benchmark had never measured the production manager at all — is
not. Batch 0 parks exactly **one** constructor goroutine; batch 1 does `CompareAndSwap(0, 1)`, which
succeeds, and every later batch follows it. With two or more constructors the remaining ones carry
the whole load.

Measured rather than argued, on the same machine, `no-dep` shape, 20,000 iterations: batch IDs from 0
give `global-local-2` **247,439 tx/s**, and from 1 give `global-local-1` **249,222 tx/s**. Same number
within noise. The pre-fix table held only the 2- and 4-constructor cases, so **no case in it hung**,
and the 47.6% cluster comparison it was said to invalidate had a valid in-process counterpart all
along.

What the fix actually bought is the `workers: 1` case, which does hang with IDs from 0 — verified, it
times out — and which did not exist until the sweep was widened to 1 / 8 / 16 / 32. That sweep is the
result worth keeping: the local constructor pool does not bound the default manager at all, since 1
through 32 constructors give 216,886 / 249,691 / 220,000 / 246,929 / 221,484 / 228,068 tx/s with no
trend, because the ceiling is in the global manager's two single goroutines.

The error was reading a goroutine dump — 1.1% CPU, a constructor parked in `sync.Cond.Wait` for five
minutes — as the state of the whole benchmark rather than of one of its goroutines, and never running
the pre-fix case to completion to check. A hypothesis about why something hangs is testable by letting
it run.

**Every figure here was measured on a workload that only ever inserts new keys, and the attempt to
measure anything else failed.** With `key-backref-rate` at 0, each transaction's two read-write
slots get fresh unique keys: no two transactions touch the same key, so the dependency graph tracks
transactions that cannot conflict and the MVCC validator never aborts one. Both graph managers were
compared on that workload and only on that workload.

Turning key reuse on collapsed throughput from 486,941 tps to about 10,000, and the first two
explanations offered for it — the dependency graph serialising, then the database — were both wrong.
The workload was invalid. `queries-rate` exists precisely to fetch committed versions for
back-references before signing, and it was left at 0, so every back-reference carried a **nil
version**. The validator classifies a nil-version write as a new key and routes it to `insert_ns`,
where a key that already exists can only raise `unique_violation`. That workload was not contended;
it was impossible, in half of its transactions, and what got measured was the failure path.

Nothing in this document establishes anything about behaviour under contention. Measuring it through
read-write slots needs `queries-rate` above 0 and a query service to serve it, which this inventory
does not deploy; measuring write contention alone does not, as the next paragraph shows.

A second attempt, with the back-references placed in **blind-write** slots instead, is valid: a blind
write carries no version by design, and the validator resolves it itself
(`populateVersionsAndCategorizeBlindWrites` looks up each key's current version and routes the ones
that exist to `update_ns`). Aborts were zero, confirming nothing impossible was submitted. Throughput
was 13,160 tps.

That one has a root cause, and it is not contention either. A control with the same blind-write layout
and `key-backref-rate` back at 0 -- so no key is ever reused, and the lookup returns nothing -- gives
12,987 tps. Key reuse is irrelevant; the collapse comes from the slot type, because blind writes are
what make the validator perform a multi-key lookup at all.

That lookup is `SELECT key, version FROM ns_X WHERE key = ANY($1)`, and on this cluster it issued one
storage read request **per key**. The reason is a deployment setting added earlier in this evaluation:
the state tables were pre-split into 120 tablets (ten per tablet server) to raise write concurrency.
Measured on identical tables holding identical rows, queried for the same 1,200 existing keys and
differing only in the split, 120 tablets cost 1,200 storage read requests and 7,801 ms where the
default split cost 2 requests and 12.5 ms -- a factor of 622, of which only 440 ms is storage work and
the rest serialised round trips. The plan is a correct primary-key index scan either way, so only the
request counts from `EXPLAIN (ANALYZE, DIST)` show it.

The setting did what it was chosen for, and its cost was unobservable for as long as every transaction
only inserted fresh keys, because nothing then performs a multi-key lookup.

It is a trade-off rather than a mistake, which a control run establishes. Dropping to 12 tablets takes
the blind-write workload from 13,160 to 314,336 tps, and takes the insert-only workload the other way,
from 486,941 to 359,866:

| Split | Insert-only | With a multi-key read |
|---|---|---|
| 120 tablets | **486,941** | 13,160 |
| 12 tablets | 359,866 | **314,336** |

So 120 tablets is worth 35% on inserts and costs a factor of 24 on multi-key reads. Worth stating
plainly because the evaluation nearly ended with a recommendation of 12 tablets as a pure 23x gain:
every headline figure in this document was measured on the insert-only workload, and that
recommendation would have cost a third of it.

The cliff is sharp rather than gradual -- 48 tablets already commits only 31,132 tps against 314,336
at 12 -- and what governs it is not the tablet count but the **product of tablets and keys per
lookup**, which must stay under roughly 32,768. That model was fitted to the tablet sweep and then
tested against predictions: 12 tablets with 2,000 keys and with 2,600 keys batch as predicted, 12
tablets with 3,000 keys breaks as predicted, and 6 tablets with 5,000 keys batches as predicted, four
for four including a pair straddling the boundary by 15%. `docs/performance-tuning.md` carries the
table and the caveat that the constant is not portable.

The useful consequence is that the committed batch width, a knob this evaluation never touched, is the
lever that lasts -- and the tablet count is not one at all. YugabyteDB splits tablets automatically as
a table grows: `ns_0`, created with 120, held 288 after eleven hours of load, and `tx_status` had gone
from 120 to 212. A table created with 12 passes 29 on its own. So the batching budget shrinks over the
life of a deployment with nobody changing a setting -- at 288 tablets it is about 114 keys per lookup,
roughly 57 transactions per batch -- and lowering the initial tablet count postpones the cliff rather
than removing it.

**Write contention, measured once the tablet cliff is out of the way, costs about 19%.** With 12
tablets and blind writes, `key-backref-rate` 0.5 commits 254,221 tps against 314,336 at
`key-backref-rate` 0 -- the same deployment, differing only in whether half the transactions write a
key an earlier transaction created. Aborts are zero, the graph carries 13,186 dependent transactions
where it carried none before, and database batch commit rises from about 205 ms to 269 ms as half the
writes become updates rather than inserts.

That is the shape one would hope for: real dependencies cost a fifth of throughput, not a factor of
24. The factor of 24 was the tablet setting, and nothing else.

What that measures is write contention alone. Blind writes cannot abort on a stale version, because
the validator resolves their versions itself at commit time, so nothing here exercises MVCC read
conflicts. Measuring those needs read-write slots carrying real versions, which needs `queries-rate`
above 0 and a query service this inventory does not deploy. That remains the one unmeasured axis.

One further finding survives, because it is a property of the commit path rather than of the
workload. `insert_ns` issues a single bulk `INSERT` for the whole batch, so **one** pre-existing key
raises `unique_violation`, rolls the whole statement back, triggers a rescan for every existing key
in the batch, discards the work of all the non-conflicting transactions along with it, and makes
`committer.go` retry the entire batch — a loop bounded at 1024 rounds. Any client that blind-writes a
key whose version it did not read pays that, and one such transaction is enough to penalise the
thousands batched with it.

The lesson for anyone reading the headline figures is narrow and firm: they describe a pipeline
carrying transactions that cannot conflict and that only ever create keys, roughly 98% of the
throughput depends on that, and the comparison between the two graph managers is established for
that case alone.

## 9. What the conflict workload actually costs, priced rather than inferred

Every conflict share, at every reference gap and both signature schemes, drains near 20,000 tps against
518,000 conflict-free. Five explanations were tried and four were wrong. This section records the
measurement that priced it, and each retraction, because the wrong ones were each consistent with the
evidence available at the time.

**The measurement.** Two `/metrics` snapshots 40 s apart from one validator-committer, during a 5%
double-spend run at the deployment's 120-way tablet split. Ratios of counters, not a rate ladder:

| quantity | value |
|---|---|
| `insert_new_key_with_value_latency` | **1.69 s** per call |
| `tx_batch_commit_latency` | 0.023 s per commit |
| insert calls per commit | **1.91** |
| batch width | 175 tx = **349 keys**, steady (cumulative 179) |
| throughput | 3,447 tps per VC, 20,681 over six |

The first two lines are the finding, and the second one is not merely smaller than the first — it was
**excluding the very batches that were slow**. `commit()` returned at its conflict path *before* reaching
its `Observe`, so a conflicted batch never entered `tx_batch_commit_latency` at all: the 22.9 ms was an
average over only the batches that never conflicted, while the conflicting attempts on the same batches
cost 1.7 s. The same missing observation was in `updateStates` and `insertTxStatus`. That is what supported
"the database is fast" through three wrong explanations, and it is now fixed — the observations are
deferred so every outcome counts, and a separate `vcservice_database_tx_batch_commit_conflict_latency_seconds`
keeps the expensive path out of the common one's average — **in the code; a `/metrics` read shows it is not
on the binary these runs used**, which is why every per-attempt cost below is still blended. `insertStates` was always observed on a `defer`,
which is the only reason the 1.7 s was visible at all.

**The mechanism.** `insert_ns_<ns>` attempts a bulk insert, and on any existing key its
`EXCEPTION WHEN unique_violation` handler runs `key = ANY(_keys)` over *every* key in the batch. The
whole database transaction then rolls back and the batch is redone — which is the 1.91 calls. That
lookup is where the tablet split acts. The first attribution was the batching threshold of §6 — 349 keys x
120 tablets = 41,880, over ~32,768, so one storage read per key — which predicted that **88 tablets would
recover and 96 would not**. The boundary test refuted it for this path. At 88 tablets the in-window width is
150.5 tx = 301 keys, so the product is 26,488, comfortably *under* the crossing where the lookup should batch
into a single round trip of some 20 ms. The insert still costs **1.194 s**, and capacity is 27,670 tps,
measured twice at two offered rates.

**What fits, and what these two points can actually separate.** The widths first, because the figure
that circulated was wrong by the retry count. `vcservice_committed_transaction_total` over
`..._insert_new_key_with_value_latency_seconds_count` is 84.2 transactions at 88 tablets, and that was read
as the batch width. It is the width *divided by the attempts*: `insertStates` passes the whole namespace's
new-write keys on every attempt (`service/vc/database.go:381`), and the retry loop rebuilds the batch minus
only the transactions the previous attempt invalidated (`service/vc/committer.go:137`), so attempt two
carries 95% of attempt one's keys rather than 56% of them. Per successful commit, over the 225 saturated
intervals: **150.5 transactions — 316 keys on the first attempt, 301 on the second.**

So the width did not halve between the two tablet counts, and the discrimination rests on that. Both rows
below count keys the same way, as twice the committed transactions per commit, which is attempt two's width;
counting attempt one's instead scales both by the same 5% abort share and moves nothing:

| model fitted at 88 tablets | predicts 120 tablets | against 1.700 s measured |
|---|---|---|
| per tablet | 1.628 s | −4.2% |
| per key x tablet | 1.893 s | +11.4% |
| per key | 1.388 s | −18.3% |

Per-tablet is the best of the three and per-key is refuted outright. But 301 keys against 350 is a 16%
difference in width, and two points that close cannot separate the first two rows: the claim of "4% against
98%" that briefly stood here came from the divided width, and on the real one the margin is 4% against 11%.

**The discriminating test holds tablets fixed, and the data for it was already collected.** Across 384
thirty-second intervals of the 88-tablet run the width moves from 287 to 535 keys on its own, as the search
steps the offered rate. Regressing insert latency on it gives `1.009 s + 0.52 µs per key` with
**R² = 0.038** — width explains 4% of the variance. By quintile:

| keys per batch | insert | ms per key | ms per tablet |
|---|---|---|---|
| 287 | 1.141 s | 3.98 | 12.97 |
| 300 | 1.195 s | 3.98 | 13.58 |
| 312 | 1.238 s | 3.96 | 14.07 |
| 346 | 1.285 s | 3.72 | 14.60 |
| 535 | 1.303 s | 2.44 | 14.80 |

1.9x the keys moves the insert by 14%. That is one tablet count, one run, one deployment, so it is not
exposed to the day-to-day drift or the bring-up differences the cross-tablet comparison carries — which
makes it the better reason to believe the cost is per-tablet. What it does not support is per-tablet
*exactly*: ms per tablet still climbs 14% across the sweep, so a weak per-key term sits on top of a
dominant fixed one, and the fixed one is what the model should be built on.

**The threshold model is refuted for this path on its own evidence**, independent of the fitting above.
301 keys x 88 tablets = 26,488 is *under* 32,768 and the insert costs 1.2 s; 350 x 120 = 42,000 is over it,
and the two differ by 42%. A boundary worth a factor of 24 on §6's read path is not what a 42% step is made
of. The 32,768 constant and the 93-tablet edge should be read as **refuted for the conflict path** rather
than merely unconfirmed, with `tab88` as the refutation.

**The failure path costs about 2.6 s, not the 1.2 s quoted above and everywhere else.**
`..._insert_new_key_with_value_latency_seconds` is one histogram over both outcomes, and the conflict
histogram that would separate them is not on the deployed binary — a `/metrics` read returns no such
series. At 1.79 attempts per successful commit there are 0.79 failing attempts against one clean one, so
backing the clean attempt out at 23–300 ms puts the failing attempt at **2.3–2.7 s**. Every per-attempt
number in this section is blended in that direction. The *ratios* between tablet counts survive it, because
the attempt mix holds within 4% across the sweep, but no absolute per-attempt cost here should be quoted
until that histogram is deployed and read.

**`tab96` arrived and it does not fit, and the reason invalidates the comparison rather than the law.**
Three probes descending from 30,000 all missed with the backlog still growing, so 96 tablets retires about
20,000 tps — level with 120, where a per-tablet law predicts 88/96 x 27,400 = 25,100. Then the drain between
probes gave the answer:

| point | tx/s per VC | keys/batch | attempts | insert | ms per tablet | concurrent calls |
|---|---|---|---|---|---|---|
| 88 tablets, saturated | 4,567 | 301 | 1.79 | 1.194 s | 13.6 | 65 |
| 96 tablets, saturated | 3,470 | 611 | 1.90 | 2.050 s | 21.4 | 44 |
| **96 tablets, draining** | 300 | 513 | 1.73 | **1.325 s** | **13.8** | 2.7 |
| 120 tablets, saturated | 3,400 | 350 | 1.91 | 1.690 s | 14.1 | 63 |

The third row is the same tablet count and the same deployment as the second, at a width 19% narrower, and
the insert is **55% cheaper** — landing exactly on the 13.6–14.2 ms per tablet the other rows fit. So
`insert_..._latency` under conflicts is **not a per-batch service time**: it carries the queueing at the
database, and how much depends on how far above capacity the probe sat.

That confounds the cross-tablet fit, and reading the probes' offered rates shows how badly. The `tab88`
batch inherited a 250,000 seed, so its four rows were taken at **5.3x to 8.6x capacity**:

| batch | offered | retired | x capacity | grow | mean latency | backlog |
|---|---|---|---|---|---|---|
| tab88 | 250,000 | 27,672 | 8.6 | **−1,111/s** | 109.3 s | 3.18M |
| tab88 | 212,500 | 27,673 | 7.3 | −111/s | 109.7 s | 3.19M |
| tab88 | 180,625 | 24,906 | 6.2 | 1,222/s | 109.9 s | 3.20M |
| tab96 | 30,000 | 28,537 | 1.4 | 4,333/s | 35.1 s | 0.73M |
| tab96 | 25,500 | 21,793 | 1.2 | 1,444/s | 12.3 s | 0.26M |
| tab96 | 21,675 | 19,544 | 1.0 | 667/s | 8.6 s | 0.18M |

**So `grow` near zero is not a test for saturation** — it reads −1,111/s at 8.6x capacity, because the queue
is pinned at a ceiling and a pinned queue has zero derivative. Any rule of the form "fit only rows whose
in-flight growth is flat" selects *these* rows, the worst four available. Mean latency, or offered against
retired, is what orders them, and by that measure every 88-tablet row carries a 3.2M backlog.

It also refutes the obvious explanation for the widths, which was mine: a deeper queue does *not* make the
batcher pick up more per cycle. The 88-tablet rows sit on a 3.2M backlog at 300 keys, while `tab96`'s 611-key
rows sit on 0.73M — the deeper queue has the narrower batch, by a factor of two in each direction. What still
differs between the two batches is the offered rate itself, 5-9x capacity against 1.0-1.4x, and nothing
measured here says why that should set width. I am not proposing a mechanism for it; five have already been
retracted in this section.

Note also that 88 saturated carries *more* concurrency than 96 saturated (65 calls against 44) at *less*
latency, so queueing inside the insert does not explain the 88 -> 96 drop either.

**What survives, and what the sweep needs instead.** Surviving: the threshold model is refuted, now on three
tablet counts with no step of the factor-of-24 kind anywhere; the failure path costs seconds; and 8 tablets
is worth more than an order of magnitude. Retirement rates survive too — a saturated pipeline retires at
capacity whatever the queue depth — so 27,400 at 88 tablets and ~20,800 at 96 stand. Not surviving: any
per-batch cost law, including the per-tablet one recorded above, and any capacity prediction derived from
one. Every row in the sweep was taken between 1.0x and 8.6x capacity, and there is **no unsaturated row at
any tablet count** — the closest is a drain read, not a measurement.

**Three variables move together in every probe, and one of them can now be eliminated.** Tablet count, batch
width and utilization all differ between any two rows in this sweep, which is why three sessions have each
fitted a law and had it refuted by the next batch — the 32,768 product, per-tablet, and per-key-x-tablet.
`tab96`'s own four probes make the point without any cross-tablet comparison: at fixed tablets and a width
held at 601-611 keys, `db_insert` still moves 2.316 -> 2.033 -> 1.850 s as the offered rate falls, so
utilization alone is worth 20% at fixed width. And its first probe, the narrowest at 312 keys, is the
*cheapest* at 1.317 s, so width and utilization are not even ordered the same way across the batch.

The variable that is *not* responsible is table size. Over the 225 steady-width intervals of the 88-tablet
run the table grew **8.8x**, from 637,012 rows to 5,595,519, at a fixed 300-305 keys per batch, and the
insert moved **+0.8%**:

| rows in table | insert | keys |
|---|---|---|
| 637,012 | 1.194 s | 301 |
| 1,856,263 | 1.165 s | 300 |
| 3,285,926 | 1.192 s | 305 |
| 4,506,654 | 1.202 s | 303 |
| 5,595,519 | 1.204 s | 301 |

So the §9 fill control holds on the failure path as well as the conflict-free one, and a designed experiment
does not need to control for how long the run has been going or how full the table is. That leaves tablets,
width and utilization — and no row anywhere in the sweep holds two of them fixed across a change in the
third, which is the whole reason nothing is identifiable.

The fix is a design change rather than more points: run every tablet count at **the same offered rate, below
every capacity in the sweep** — 15,000 tps clears 20,000 at 96 and 120 and 27,400 at 88 — for 300 s, and read
width and insert in-window. That holds the backlog near zero at every point, so what differs between them is
the tablet count. Any conclusion about how this path scales should wait for it. `db_insert` also reached the
driver's queries only for `tab88`, so even the two-point version is thinner than it looks.

The per-tablet reading also reprices the fix. If the handler's `key = ANY(_keys)` cannot prune tablets and
pays a round trip to each regardless of how many keys it seeks, then `ON CONFLICT DO NOTHING ... RETURNING`
does not shrink the fan-out, it **removes** it — together with the rollback, which must also reach every
tablet the batch touched. That is a larger claim for the fix than the batching story made, and it still
needs sanction.

The 8-tablet point needs no special case under either reading: one round trip to each of 8 rather than 120
is a fifteenth of the fan-out, which is the order of the measured speed-up.

**This does not overturn §6's threshold result, and the difference is which query is being paid for.** That
model was fitted to `queryVersionsIfPresent` on the blind-write workload and tested four for four, including
a pair straddling the boundary by 15%. Both can hold if the conflict path's cost is not dominated by
batched-versus-unbatched reads at all — the exception handler also rolls the whole distributed transaction
back, and an abort must reach every tablet the batch touched, which is continuous in tablet count with no
threshold to cross. That is a hypothesis about *which* per-tablet work dominates, not a retraction of either
measurement, and it predicts that the conflict path's cost should track tablets even at widths far below the
crossing.

Capacity then follows with one further term, and it is constant across both points:

    throughput = (concurrent batches x width) / (calls per commit x insert latency)

    120 tablets   20,400 x 1.91 x 1.690 / 175 = 376 batches = 63 per VC
     88 tablets   27,670 x 1.78 x 1.170 / 148 = 389 batches = 65 per VC

So the pipeline holds ~64 batches per validator-committer in flight whatever the tablet count, and capacity is
set entirely by how long an insert attempt takes. **Recorded before that batch ran**, and revised once from the
corrected 88-tablet row while still ahead of the data: at 96 tablets, if the width holds near 308 keys, the
product is 29,568, so insert ≈ **1.32 s** and capacity ≈ **25,000 tps**. It no longer separates the two models
— 96 was under the crossing too — so it tests only whether the per-key-tablet constant is constant.

That batch is also the first at a high tablet count that *can* certify an operating rate, because it is
seeded at 30,000 rather than 250,000. A rate search descends x0.85 six times, so its floor is 0.377 of the
seed: from 250,000 that floor is 94,286, three and a half times the capacity, and every probe saturates and
misses on latency while the batch reports "no rate met" having established nothing. From 30,000 the floor is
11,313 and the knee is bracketed in two or three probes. Seeds carried over from the conflict-free shape,
where 480,000 was right, are nine times wrong for a workload that retires 27,000 — which is the same class of
fault as the drain default: nothing errors, the log fills with plausible probe lines, and the run certifies
nothing.

It is invisible on the headline workload because inserting only fresh keys never raises the exception,
so nothing ever performs a multi-key lookup. A back-reference is the first thing that does.

**The control that rules out table fill.** A workload that inserts millions of rows and slows down is
supposed to be table fill, and that is the one alternative every other test above leaves standing. It
does not survive its own control. Counter ratios are insensitive to a backlog, so the same
validator-committer was sampled twice in the same run at two table sizes:

| | table | width | calls/commit | insert | tps/VC |
|---|---|---|---|---|---|
| early | ~4.6M rows | 175 tx | 1.91 | **1.690 s** | 3,447 |
| later | ~7.4M rows | 172 tx | 1.93 | **1.646 s** | 3,454 |

The table grew **61%** and the insert cost moved 2.6%, downward — inside noise, and the wrong way for
fill. So the cost is per-attempt work, not accumulated state: the number of unbatched reads an attempt
issues, which is fixed by keys per batch and tablets and does not care how much data is already there.
That is also what makes the 88-versus-96-tablet boundary a real test rather than a curiosity, since a
fill-driven effect would move the edge as the run proceeds and a per-attempt one will not.

**Why 349 keys is not a knob.** `service/vc` has **no maximum batch size at all** — only
`MinTransactionBatchSize` (a floor, default 1) and `TimeoutForMinTransactionBatchSize` (5 s).
`batchReceivedTransactionsAndForwardForProcessing` accumulates into `largerBatch` and sends on the floor
or the timer, so with a floor of 1 it dispatches as soon as anything is queued and never reaches the
timer under load. Batch width is therefore a *consequence* of the downstream service rate, not a
setting. Capping it is a code change, and the narrower fix is the SQL: `INSERT ... ON CONFLICT DO NOTHING
... RETURNING` identifies the offending keys without an exception and without a lookup over every key in
the batch.

Three things about that fix, since its cost is what decides whether it is worth proposing. It does **not**
change the function's contract: computing the violating set in the same statement, as an anti-join between
the unnested input and the returned keys, leaves `insert_ns` returning violating keys exactly as it does
now, so `insertStates`, the retry loop and the existing tests are untouched. It introduces **no new
write-then-revert**: the conflict path already runs `defer rollBackFunc()` over the whole transaction
(`database.go:243`, taken at the early return two lines below), so partial inserts are discarded today by
the same rollback, for the same reason — no new mechanism and no new tombstones. And it fixes a latent
correctness bug rather than only a performance one: if a batch ever holds two writes to the same new key,
today's handler finds neither in the table, returns an empty violating set, and the caller commits a
transaction whose insert the exception had already rolled back — silently dropping both writes. The
dependency graph should prevent such a batch forming, so it is unreachable rather than broken; `DO NOTHING`
makes it correct instead of merely unreachable.

**Sanctioned and written (2026-09-14).** `utils/statedb/create_namespace_tmpl.sql` now uses
`ON CONFLICT (key) DO NOTHING ... RETURNING key`, with the violating set computed in the same statement as
`ARRAY(SELECT unnest(_keys) EXCEPT ALL SELECT unnest(inserted))`. No Go changed, and
`TestCommit/new_writes_with_violating` — which drives the conflict path — passes unchanged, which is the
contract claim above confirmed rather than asserted.

Three semantics verified against Postgres 18 rather than assumed, on a batch of `A,B,C,D,A` where `B` and `D`
already existed:

    RETURNING key            ->  A, C          the keys INSERTED, not the ones that collided
    _keys EXCEPT ALL ins     ->  B, D, A       both pre-existing keys, plus the duplicate's second copy
    table afterwards         ->  A=n B=old C=n D=old

The first is the one worth writing down, because the natural reading is the opposite: `ON CONFLICT (key)` is a
conflict *target* naming the index, while `RETURNING key` yields one row per row actually **affected**, and a
row skipped by `DO NOTHING` was never affected. The third is why the abort is *required* rather than tidy —
`A` and `C` persist — so if `A` and `B` belonged to one transaction, committing would half-apply it.
`DO NOTHING` also skips the intra-batch duplicate instead of erroring, unlike `DO UPDATE`, which is why the
difference is `EXCEPT ALL` and not `EXCEPT`.

**Two things still to verify, and neither is safe to assume given this section's record.** Whether
YugabyteDB's `ON CONFLICT` *avoids* the per-key reads or merely relocates them — detecting a primary-key
conflict requires reading something, and if it reads per key the cost moves rather than goes; one
`EXPLAIN (ANALYZE, DIST)` at the real batch width reading `Storage Read Requests` settles it. And that the
conflict-free path did not regress: it now materialises a `RETURNING` set and compares cardinalities where it
returned `'{}'` after a bare INSERT, on a path that runs some 3,400 times a second at 518,000 tps.

`CREATE OR REPLACE` is not a live upgrade path, which is worth stating separately: namespaces are created
once, so a cluster already holding the old function keeps it until the namespace is recreated. Fine for these
runs, which redeploy from scratch, and not fine for an upgrade.

**What was wrong, and why it looked right.** Recorded with the same weight as the findings, because the
failure mode all week has been confident mechanisms that did not survive contact: a log listing only
what stuck will get the graph limit re-proposed within the week.

- *The nil-version insert path is unique to this generator.* It is not — the published run's generator
  also wrote no version on a conflicting key, so both take the same branch.
- *The 120-way pre-split is ours and postdates the published run.* The published run had it too.
- *The reference gap is smaller than the graph's window, so references become dependencies.* The abort
  rate matches the configured share exactly, so references do land on committed keys and abort as
  intended. Blocked transactions are 2,411 of 500,000.
- *Over `waiting-txs-limit` the graph admits one batch per validation and the pipeline goes lock-step.*
  Refuted by raising the limit to 5,000,000, verified in the coordinator's rendered config: the graph
  sat at ~500,000, never near its limit, and throughput did not move. The population is bounded by
  `committer_sidecar_waiting_txs_limit`, which is also 500,000 — raising one of two equal limits could
  not have shown anything.
- *Batch width is a self-reinforcing loop: a deeper queue widens the batch, which lengthens the lookup.*
  Refuted by conjunction: in-flight rose 1.83M → 3.34M (+82%) while width went 179 → 175.
  `MinTransactionBatchSize` defaults to **1**, so the VC's batcher dispatches as soon as anything is
  queued and never reaches its 5 s timer under load. Width is arrival-per-send-cycle, set by the
  downstream service rate. There is also no maximum width anywhere in `service/vc`, so capping it is a
  code change rather than configuration — and it is the wrong lever, since 349 keys is what 175
  transactions of this shape cost.

Two readings, rather than mechanisms, are withdrawn with them, because they are what misdirected the
search rather than merely being wrong:

- *The database is idle.* Read from `commit_util` at 2–4 of 192 workers and `tx_batch_commit_latency` at
  17–28 ms. Both were true and both were the wrong instrument: the VC's workers were blocked on a
  tserver that was at 70–71% CPU, and the outer commit metric excludes the insert path that dominates it.
- *The latencies of every 5M ladder rung past the first.* Withdrawn under the drain defect below: means
  of 148,000–155,000 ms are the inherited queue, and the p99s sit on the 60,000 ms measurement ceiling,
  which is not a percentile. The **throughput** column of those rungs is not withdrawn — see below, where
  the distinction is drawn — and this entry replaces an earlier, wider withdrawal of mine that took the
  throughputs with the latencies.

- *A column read of `tiers.log`.* The cross-tier sampler's `awk` collapses a row when any single metric
  is absent, so positions shift silently and every column-wise reading of that file is unsafe. Numbers
  quoted from it — including a graph population "flat at ~505,000" — are withdrawn in favour of direct
  `/metrics` reads. The file remains useful as a continuous record; it is the parsing that is unsound.

**One harness defect found on the way, which invalidates part of the evidence above.** It has three
parts, and any one of them alone reads as a tuning nit:

1. **The park rate is absolute.** `drain()` sets `FX_DRAIN_RATE`, default **20,000**, while this
   workload retires **20,235**. A park rate is only a drain if it sits well below capacity, and nothing
   checks that it does.
2. **The early return then makes the failure silent.** `if inflight < 4 * DRAIN_RATE or (previous is not
   None and inflight >= previous): return` — at capacity ≈ park rate the backlog cannot shrink, so the
   second clause fires on the first comparison and the drain declares itself done after ~120 s. In-flight
   *rose* 1,830,000 → 3,340,000 while the log read "draining". "Cannot drain" is reported as "drained".
3. **The consequence is a rule, not an anecdote.** On any conflict ladder, **only rung 1 is quotable**,
   because rung 1 alone starts from a fresh deployment. Every later rung measures an inherited queue and
   its mean latency is that queue rather than a service time.

So "throughput is flat in offered rate" is **not established** by this ladder, and three rungs that
looked like evidence for it are withdrawn below.

**And the defect has a class, which is worth more than the instance.** By the end of this investigation
**eight** faults shared one shape: *the tool reports success while doing part of the job, and its output
falls in the range a real answer would*. That last clause is what made every one of them expensive. A tool
that returns nothing, or an obvious sentinel, is caught in minutes; a tool that returns 10 tablets on a
twelve-server cluster, or a p99 of 29,900 ms, or a drain that says it is draining, is written into a document.
Three of the eight were each independently believed by three sessions.

The eight, in the order they were found: `FX_ONLY` matching ids by prefix; a runner discarding exit codes;
`${4:+VAR=$4}` becoming a command word rather than an assignment; `drain()` declaring itself drained at a park
rate equal to capacity; `histogram_quantile` returning 99% of a bucket width as though it were a measurement;
`search()`'s seed floor at `seed x 0.85^6`, reporting "no rate met" where it means "never probed low enough";
a plot script keying on `r["id"]` where the rows carry `r["experiment"]`; and `yb-admin list_tablets` silently
truncating at `max_tablets = 10`. Two more belong to it in spirit — a sampler left on an unlinked inode by a
bring-up, logging nothing for 45 minutes while its process stayed alive, and `inflight_growth` reading flat on
a queue pinned at its ceiling.

The generalisation worth keeping is not "check the exit code". It is that **a plausible number is the failure
mode**, so the defence has to be a quantity the fault cannot fake: an artifact read back, a count compared
against something independently known, or a precondition asserted before the measurement rather than inferred
after it. Three of the eight passed every check that looked only at the tool's own report of itself.

The first three below are recorded in detail because they were the first found, not because they are the worst.

1. `FX_ONLY` matches experiment ids by **prefix**, so a filter meant for one point silently selects its
   neighbours — or, given the wrong prefix, nothing, and the matrix "completes".
2. A batch runner that **discards exit codes** turns any failed step into a completed one. A narrowed
   `make teardown` returned rc=2 on every point and raced two ladders to `MATRIX COMPLETE` in 18 seconds
   having measured nothing.
3. **An expanded assignment is not an assignment.** Bash recognises `VAR=value` at parse time, before
   expansion, so `${4:+FX_DRAIN_RATE=$4}` is never a variable assignment — the expanded word becomes the
   *command*:

   ```
   t() { A=1 ${2:+B=$2} printenv A B; }
   t x 9   ->   B=9: command not found, rc=127     # printenv never runs
   ```

   Written literally as `FX_DRAIN_RATE=${4:-20000}` it works. A chain passing a drain rate that way to
   five of seven batches would have exited 127 before starting a driver in each, and with (2) above the
   log would have called them done in seconds — the whole tablet axis, silently.

The drain defect above belongs to the same class, and it is the instructive member of it, because it
defeats the first two defences. `drain()` echoed `draining: 3,380,000 in flight` on every round: it
reported its input and its state faithfully, and still declared success while doing nothing. Nothing
compared the park rate against capacity.

So the rule has three clauses, and the third is the one that would have caught every fault here:

- **Gate on the artifact, not the exit code** — a rendered config read back, a row count in the results
  file, a `--list-hosts` before trusting a host pattern.
- **Echo the inputs a run was given**, because an input silently dropped is indistinguishable from
  success. The `redo=<none>` line exists because three ladders were lost to an `FX_REDO` the wrapper
  overrode with an empty string.
- **Assert the precondition the step depends on.** A drain that cannot drain should refuse, not return.
  A park rate at or above capacity is not a slow drain, it is a hold, and the code can know that before it
  waits four minutes to find out.

The common cost is the same: these are the only failures that cost days rather than minutes, because a
failure that announces itself is fixed in the next command. The drain defect cost four rungs; the
assignment one would have cost five batches.

**What can be said about the conflict workload today, and what cannot.** The tablet count moves this
workload by an order of magnitude, and the cost per transaction says where it goes. Basis:
busiest-host CPU% x 64 threads / **finished** transactions a second, where finished is committed plus
aborted — the same convention the throughput figures use, and it has to be stated, since the same 8-tablet
row on committed instead reads 78 µs rather than 74.

| | offered | finished | busiest CPU | CPU per transaction |
|---|---|---|---|---|
| conflict-free | — | 518,727 | 80% | **99 µs** |
| 5% conflicts, 120 tablets | 25,000 | 21,273 | 71% | **2,136 µs** |
| 5% conflicts, 8 tablets | 181,031 | 181,091 | 21% | **74 µs** |

At 120 tablets a conflicting transaction costs **22 times** what a conflict-free one does, and at 8
tablets it costs the same as one — 74 µs against 99. The work is not inherent to conflicts; it is the
unbatched lookup, and it disappears when the lookup stays batched.

**Retracted within the hour: the no-pre-split series is a different workload, not a different split.** The
claim was that `split0-ds10` commits 92,958 tps at ~0.2 s with pre-splitting off, against ~20,300 at the
120-way split, so pre-splitting costs 4.6x and the bound. The configurations are not comparable:

| series | reference gap | lookback | tablets |
|---|---|---|---|
| `split0-ds10/20/30` | **0** | 10,000,000 | 0 |
| `9c-ds5`, `9c-ds10` | 1,000 | 1,000,000 | 120 |
| `9c-ds20`, `9c-ds30`, all `tab*`, `split8` | 300,000 | 1,000,000 | 0–120 |

At gap 0 a back-reference names a key generated immediately before it, so the referent is still in flight and
the dependency graph holds the pair and serialises it — the conflict never reaches `insert_ns` as an existence
violation at all. Those runs measure graph serialisation; the `9c-ds*` runs measure the insert failure path.
Two mechanisms, one axis. `fx-plot-figures.py:351` had already removed the series from panel 9c on exactly this
ground, recorded there as "every one of them was measured with the reference gap that made the workload a
dependency convoy rather than a double spend", and the error came back because rows were matched on offered
rate rather than on configuration. It is the [[compare-like-for-like-before-blaming-a-stage]] failure with the
workload in place of the build. **The gap is not uniform even inside `9c-ds*`**, so it has to be read off
`vars` for both sides of any conflict comparison.

**No conflicting workload has ever been offered a sub-capacity rate at the 120-way split.** Sorting every
gap-300,000 conflict row at that split by offered rate, the floor is **25,000 tps** — `ladder5m` rung 1, which
committed 20,235 at `grow +4,100` and a 69.3 s mean. Capacity is ~20,300. So all 70-odd measurements are of a
workload past its own capacity, and "no offered rate qualifies however low" was never measured: what was
measured is that a saturated conflicting workload misses the bound by two orders of magnitude.

**That finding is true of the 120-way split only, and I over-applied it.** At 96 tablets the descent reached
**11,313 tps offered against a capacity of ~20,545** — little over half — and five rungs from 11,313 to 21,675
each retired what they were offered at a flat in-flight count, giving means of 5.6 to 8.6 s. So at 96 tablets
the negative is measured across five points spanning a factor of two in rate, not extrapolated.

I had read those rows' in-flight counts as leftover backlog and that was wrong. **Little's law is an
identity**: sustaining 10,727/s at a 5.63 s latency *requires* 60,435 in flight, so the count is the working
set, not a queue to be blamed. The steady-state test is `finished` ≈ `offered` **together with** flat growth,
and the two halves separate the cases cleanly — `tab88` fails the first half (250,000 offered against 27,672
retired, so its flat growth is a pinned queue) while `tab96`'s low rungs pass both. Collapsing the test to
growth alone is what produced both errors, mine and the one I corrected.

What replaces the universal is a bounded extrapolation. Across those five rungs `db_insert` falls
**monotonically** with the offered rate — 2.033, 1.850, 1.764, 1.724, 1.619 s as the rate halves from 21,675
to 11,313 — and it is flattening. Fitting inside a second at 1.90 attempts needs **0.53 s**, a further
threefold fall against a trend that gave 20% for the first halving. The 30,000 rung's 1.317 s is excluded: it
is the deeply saturated one at growth +4,333 that has been anomalous all day.

What settles it is `ladderlow` and `tabhold` at 10,000 and 15,000, which are the first sub-capacity rates the
120-way split will have been offered. And the first `nosplit` rung shows the question is live rather than
academic: 5% double spends with pre-splitting off, 15,000 offered, 15,091 finished, 14,354 committed at a 4.9%
abort share, **p99 408 ms**, growth 0, busiest host **2% CPU** — verified from `vars` as gap 300,000 and
lookback 1,000,000, so it is the valid configuration. A conflicting workload does have a sub-second operating
point at some layout, at a rate the 120-way split has never been given.

**The tablet count is not a property of the configuration — it is a function of time, and three sessions
each measured it once and disagreed.** Reads of `ns_0` on a table created with no split clause:

| when | count | via |
|---|---|---|
| 3 minutes after bring-up | **2** | `yb-admin list_tablets ... 0` |
| ~26 minutes in, during the ds5 ladder | **12** | `yb-admin list_tablets ... 0` |
| later, and holding | **23** | master `/api/v1/table` |

YugabyteDB splits a table as it grows, so all three are correct at the times they were taken; the apparent
contradiction needed no explanation about leaders, replicas or hidden states. The trajectory even matches the
documented phase defaults — low phase is one tablet per server, **12** on twelve servers, at a 128 MiB
threshold, and high phase runs to 24 per server at 10 GiB — so 12 is the boundary the table crossed rather
than a resting point. The same is true at the other end of the axis: `ns_0` created with 120 held **288** after
eleven hours.

**So `committer_database_table_pre_split_tablets` names a starting point, not a layout,** and a figure column
headed "tablets" is wrong wherever it is not qualified. The document now heads it "pre-split" and says the
count drifts upward from it.

**Every row of the tablet axis carries this, not just the nosplit one.** The 88, 96 and 120 rows were each
measured at some unknown count *above* their label, differing by row and by how long the run had been going —
`ns_0` created with 120 sitting at 288 after eleven hours is the scale of it. That does not weaken the layout
result, which is a comparison of settings and is what an operator actually chooses; it means **the only clean
tablet axis obtainable from this deployment is a pinned one**, with automatic splitting disabled. Unpinned,
`tabhold` cannot do the job it was designed for and would produce six more starting-value labels.

That this did not corrupt the ds5 ladder is worth stating, because the count grew roughly sixfold *during*
it: insert cost across the four rungs ran 15.2, 13.9, 13.7, 13.6 ms with attempts 1.911 → 1.907 and keys per
call 356 → 355. It fell by a ninth while both the offered rate rose 6.7-fold and the table split underneath
it. Both of those push the cost *up* if either matters — at a 96-way pre-split a mere doubling of rate moves
the insert 1.62 → 2.03 s — so two upward pressures produced a downward ninth. That is insensitivity rather
than cancellation, and it brackets the cliff **above 23** rather than merely "not below": the crossing sits
between roughly 23 and 88, which makes `tabhold`'s 32 and 48 rungs the informative ones and 96 and 120
confirmatory.

**A secondary trap found on the way: `yb-admin list_tablets` truncates at ten.**
It takes an optional `max_tablets` argument that **defaults to 10**, so the obvious invocation truncates and
returns a plausible number with no warning:

    yb-admin ... list_tablets ysql.yugabyte ns_0        ->  10   (truncated at the default)
    yb-admin ... list_tablets ysql.yugabyte ns_0 0      ->  12   (0 means max)

Its own usage string says `[<max_tablets>] (default 10, set 0 for max)`, which is only visible if the command
is run with no table and made to fail. Pass `0` for the real count at that instant. This is what produced the
"twelve tablets" reading above; the number was right for its moment and wrong as a label.

**The rate-matched pair, which is the strongest thing in the dataset.** Verified field by field:
`committer_database_table_pre_split_tablets` is the *only* entry in `vars` that differs — reference gap,
lookback, conflict share, shape, block producer and every other field identical.

| | offered | finished | growth | cpu | µs/tx | `db_insert` | latency | |
|---|---|---|---|---|---|---|---|---|
| 12 tablets | 15,000 | 15,091 | 0 | 1.9% | 79 | **15.2 ms** | 408 ms p99 | met |
| 96 tablets | 15,659 | 15,636 | 0 | 33.8% | 1,382 | **1,764 ms** | 6,935 ms mean | missed |

Offered within 4.4%, retired within 3.6%, the same 4.88% abort share, both fully retiring at flat in-flight.
**116x on the insert and 17.6x on CPU per transaction, from one variable.** This is immune to every confound
raised against the tablet sweep — utilization, batch width, reference gap, table fill — because they are all
held equal by construction rather than by argument. It also settles the load-dependence question by
subordinating it: the load term is worth 20% across a halving of rate, against 116x here, so it is real and
negligible beside the layout term it had been obscuring.

Note it does **not** revive a per-tablet cost law: 15.2/12 = 1.27 ms per tablet against 1,764/96 = 18.4, a
factor of fifteen apart. `tabhold` at 32/48/64/88/96/120 on one fixed rate is what could settle that, and until
it lands the mechanism stays open while the effect is established.

**The twelve-tablet ladder, in progress at 5% double spends:** 15,000 offered → 14,354 committed at 408 ms p99
and 1.9% CPU; 30,000 → 28,537 at **192 ms** and 3%; 60,000 → 56,899 at **189 ms** and 7%. Latency is flat
across a fourfold rate increase and the busiest machine is at 7%, so the ceiling is far above — 2.8x the
120-way split's whole capacity already, on a workload that split cannot bring inside the bound at any rate
tested. The 100,000 rung is still to come, and it bears on the eight-tablet anomaly: if twelve tablets sustains
near 100,000, the 172,260 tps eight-tablet probe stops looking like an outlier and its three failed holds
become the thing needing an explanation.

**The generated conflict share is not the configured one, and the gap grows with the share.** This is the
check with real content, since the generator could produce a share other than the one it was asked for — and
the panel's own comment records a case where 5% generated 2.6%. Every gap-300,000 row, grouped by setting:

| configured | rows | generated | range | relative shortfall |
|---|---|---|---|---|
| 5% | 84 | 4.876% | 4.87–4.88% | 2.5% |
| 10% | 9 | 9.513% | 9.51–9.52% | 4.9% |
| 20% | 7 | 18.089% | 18.07–18.11% | 9.6% |
| 30% | 7 | 25.828% | 25.81–25.85% | 13.9% |

Two things follow. It is **deterministic** — three significant figures across 84 rows at 5%, spanning
different pre-splits, offered rates, block producers and days, with a spread of one part in five hundred. And
it is **not negligible at the top of the axis**: a bar labelled 30% rejects 25.8%, so the axis has to carry
what was generated, which is what `measured_conflicts` and the figure caption already do.

**The end-to-end arm's reported numbers were audited against `figures-orderer.jsonl` and are clean.** Recorded
so it is not repeated: 499,091 tps at a 447 ms median and 658 ms p99 is a 300-second window, fully retired,
with zero aborts at every rung; every rate below it arrived within 0.2% (350,000 → 350,000; 400,000 → 399,636;
450,000 → 450,182); eight shards reach 500,000 against four shards' 499,091, 0.18% apart; four shards hold the
lower p99 at both matched rates (658 against 754 ms at 500,000, 547 against 684 at 450,000); the busiest
machine is `commit6`/`commit5` at the top of both ladders and a router below, which is what "the knee is not an
ordering component" rests on; 81.6% is the true maximum behind "no machine exceeds 82%", with the load
generator at 76.7%; and the 250,000 tps small-batch ceiling reads `blk_rate` **499.98**, so "a block-rate limit
near 500 blocks a second" is measured rather than inferred. Also checked: after the reference-gap filter, no
plotted point in either arm mixes configurations within an x.

**A rule for proposing mechanisms at all, since five in this section were retracted and the next one is
below.** The five that died were each *fitted to the quantity they then explained*: a curve was drawn through
the numbers and the numbers were offered as its confirmation. The one that survives was **derived from the
code with a named causal chain, and then tested against a variable that was absent from the fit**. That is the
test to apply to the next candidate, and it is cheap: ask what the proposed mechanism forbids, then look for
that.

**`a = p(1 - p/2)` is derivable from the generator, and it is the one mechanism in this section worth
proposing.** `slotKeys` (`loadgen/workload/tx_rand.go:139`) sets `newKeysRate = slotsPerTx - KeyBackrefRate`
and takes `newKeys = min(slots, frontier(i+1) - frontier(i))`, so with two read-write slots a fraction `p` of
transactions get one fresh key and one back-reference. That alone would abort exactly `p`. The correction is a
feedback the code's own comment names — *"only writes advance committed state, but the frontier still counts
every index it hands out"*: an aborted transaction's fresh key is rolled back, so its index was handed out and
never committed. Conflicts leave holes, and a later reference landing in a hole finds no key, raises no
`unique_violation`, and commits. Conflicts suppress conflicts. With `2 - p` indices handed out per transaction
and `a` holes among them, `a = p(1 - a/(2 - p))`, whose fixed point is `a = p(2 - p)/2 = p(1 - p/2)`.

| configured | derived | generated | residual |
|---|---|---|---|
| 5% | 4.875% | 4.876% | +0.02% |
| 10% | 9.500% | 9.513% | +0.14% |
| 20% | 18.000% | 18.089% | +0.49% |
| 30% | 25.500% | 25.828% | +1.3% |

**The derivation came after the curve was noticed, so the four-point agreement is not evidence for it.** Two
things that are:

1. **It contains no layout term, and the measurement has none.** At a configured 5% the generated share reads
   4.878% at no pre-split, 4.876% at 8, 4.876% at 88, 4.874% at 96 and 4.876% at 120 — a spread of 0.017
   percentage points across 84 rows, while committed throughput across those same configurations varies by a
   factor of five. Nothing in `p(1 - p/2)` could have produced a layout dependence, and none is there.
2. **The residuals are all positive and grow monotonically with `p`**, which is what a first-order treatment
   should leave: the derivation assumes a reference meets at most one hole and that the hole fraction is
   global rather than computed over the lookback window, and both approximations fail in the same direction as
   `p` rises. Alternating signs would have refuted it.

This workload also has no competing source of holes. The comment's "harmless wasted index" arises from
*read-only* slots, and `shape(2, 0)` configures none — so the aborted-transaction rollback is the only hole
former here, which is why the first-order treatment does as well as it does.

**Pre-registered, before the batch runs: `9c-ds5-rw4` should abort 4.817%, not 4.876%.** This is the test the
derivation deserves, because slot count is a variable absent from every fit made to it. Expanding the general
form for small `p` gives `a ~= p(1 - p(s-1)/s)`, so at fixed `p` the shortfall *grows* with slot count even
though the `p = 1` ceiling does not move:

    s = 2, p = 0.05:  0.05 x 1.95 / 2.00           = 4.875%   (measured 4.876% over 84 rows)
    s = 4, p = 0.05:  0.05 x 3.95 / (4 + 0.10)     = 4.817%   (predicted)

A 1.2% relative separation against a measurement spread of 0.017 pp — 0.35% relative — so roughly three and a
half times the noise, resolvable in a single rung. Per the residual pattern at `s = 2` the observed value
should land slightly *above* the derived one, so **4.82-4.84%**.

Checked before registering, because three claims died today to unchecked configuration: `9c-ds5-rw4` is
`shape(4, 0, backref=0.05)`, and `shape()` sets gap 300,000 and a 1,000,000-key lookback for any non-zero
backref — identical to every `s = 2` row, with `loadgen_read_write_tx_keys` the only difference. The window
covers fewer *transactions* at four slots, since the frontier advances twice as fast, but holes are uniform in
index space so the hole density it sees is unchanged and the derivation is unaffected.

Three ways it can fail, each more informative than confirmation: **4.876%** says slot count does not enter and
the `s - 1` term is wrong; **below 4.80%** says the shortfall is steeper in `s` than first order; **far off
either** says hole formation is a coincidence that happened to fit `s = 2`. Note the batch was queued to test a
*cost* law whose premise — that a tablet law exists to attribute keys to — is the one three sessions have now
had refuted, so this share prediction may be the more useful half of it.

**Not claimed:** that hole formation is the only contributor. Pre-genesis negative indices and within-window
sampling collisions push the same way and are unquantified. The falsifiable prediction is that the effect
vanishes if the frontier stops counting indices whose transaction aborted — a code change, measurable.

The practical statement is unchanged and is what a reader needs: read the share off the measurement, never off
the configuration. The derivation only means it is now predictable rather than merely reproducible — and it
gives two things a future conflict experiment can use, neither needing a run.

**It inverts, so a round share can be configured for.** `p = 1 - sqrt(1 - 2a)`, which round-trips to four
decimals: configure 5.132% to measure 5%, 10.557% for 10%, 22.540% for 20%, 36.754% for 30%. Every point in
this sweep was configured round and measured non-round; the reverse is available and matters most at the top,
where configuring 30% misses by four percentage points.

**And the reachable share saturates at 50%, whatever the shape.** The general form needs one term the two-slot
case hides — an aborted transaction rolls back *all* its fresh keys, and it has `s - 1` of them, not one, so
holes per transaction are `a(s - 1)`:

    a = p(s - p) / (s + p(s - 2))          collapses to p(2 - p)/2 at s = 2

At `p = 1` that is `(s - 1) / (2s - 2)` = **exactly 1/2 for every `s` >= 2**. More read-write slots buy more
references per transaction and also more holes per abort, and the two cancel identically — so widening the
shape is not a route to a higher share, which would have been a wasted experiment. The 50% is a property of
the feedback rather than of the configuration: at one reference per transaction, half the references land in
holes left by the other half's failures.

Above `p = 1` the derivation does not apply, and the curve says so itself: `p(2 - p)/2` is symmetric about
`p = 1`, predicting the same 49.5% at 0.9 and at 1.1. More references producing fewer conflicts is not
physical, so the model's domain is visible in its own geometry. `config.go:128` gates only on
`KeyBackrefRate > totalSlots`, so 1 < `p` <= 2 is configurable and unaccounted for.

**At the no-pre-split setting the conflict share is bookkeeping, and the evidence for that is not the
arithmetic it first looked like.** Rung 2 of each ladder, same offered rate and layout:

| | offered | finished | abort | committed | p99 | CPU |
|---|---|---|---|---|---|---|
| 5% | 30,000 | 30,000 | 4.88% | 28,537 | 192 ms | 3% |
| 10% | 30,000 | 30,000 | 9.51% | 27,147 | 150 ms | 3% |

`offered x (1 - share)` reproduces `committed` to the digit at both rungs, which looks like a law and is an
**identity**: the share is defined as `aborted / finished`, and `finished = committed + aborted`, so the
product is `finished - aborted` whenever `finished == offered`. The residual is exactly 0, not the three parts
per million a rounded share suggests. It re-derives `committed` from `committed`.

The content is in the two facts the identity assumes, and both are measured: **`finished` equals `offered` at
both shares** — 30,000 of 30,000, retired in full — and **p99 did not degrade**, 192 ms to 150 ms at 3% CPU
either way, improving in the direction an aborted transaction should, since it skips the commit work a
committed one performs. So the pipeline absorbs a doubled conflict share with no loss of retired throughput
and no latency cost: the share selects which transactions fail and charges nothing for the failing.

**ds30 ran, and the pre-registration failed on its latency half — which bounds the result.** Registered:
rung 2 retires its full offered rate at a p99 at or below ~200 ms. Outcome, with one correction to my own
registration first: I named 30,000 as rung 2 from the ds5 ladder, but ds30's rungs are 10,000/25,000/50,000/
80,000, so rung 2 was 25,000.

| share | offered | finished | abort | p50 | mean | p99 | `db_insert` | attempts | keys | |
|---|---|---|---|---|---|---|---|---|---|---|
| 20% | 25,000 | 24,909 | 18.10% | — | 140 ms | 195 ms | 11.8 ms | — | — | met |
| 30% | 10,000 | 10,000 | 25.86% | 164 ms | 163 ms | 519 ms | 14.5 ms | 1.972 | 234 | met |
| 30% | 25,000 | 25,091 | 25.84% | **130 ms** | **258 ms** | **3,565 ms** | **52.9 ms** | 1.962 | 226 | **missed** |

**The throughput half held exactly**: 25,091 of 25,000 retired, growth 0, and the abort share landed at 25.84%
against 25.828% measured earlier — so the share is still bookkeeping for *throughput* at 30%. **The latency
half failed by a factor of eighteen**, and not in the mean: the mean rose 1.8x while p99 rose 18x, so the ratio
went from 1.4 — where it had sat on every rung of every share at this layout — to 13.8.

The diagnostic came with it, and it rules out the two cheapest explanations. `db_insert` had been flat at
**11.6–15.3 ms across every rung of 5%, 10% and 20%**, independent of both rate and share; at 30% it reads
14.5 ms at 10,000 offered and **52.9 ms at 25,000**. But **attempts per commit held at 1.96 and keys per call
at ~230** across both rungs — so it is not extra retries and not wider batches. The same number of inserts, of
the same width, each became 3.65 times more expensive.

And the distribution went bimodal rather than shifting: the **median fell**, 164 to 130 ms, while the mean rose
to 258 and p99 to 3,565. Most transactions got *faster* and a minority got very slow. That is a different
failure from the 96-tablet case, where the mean and the tail moved together.

The tail is real rather than bucket resolution: 3,565 ms sits well inside the (3, 5] s bucket rather than at a
boundary, and the mean moved 58% with it. Rung 1's 519 ms is the more marginal reading — near a boundary with
an unmoved mean — so the two should not be treated as one phenomenon.

So the section's claim needs a boundary rather than a universal: **at twelve tablets the conflict share costs
nothing in throughput up to 30%, and nothing in latency up to 20% at any rate measured; at 30% it holds to
10,000 tps and the tail breaks the bound by 25,000, while the mean barely moves.**

**One caveat the section must carry**: the failing rung is one measurement on a rate list that has since been
superseded. A boundary quoted from a single row is thin, and repeating 25,000 costs one rung — which is why the
re-run should repeat it rather than only bracket it at 15,000 and 30,000. That is a narrower claim than three ladders suggested and a more useful
one, and it is exactly what the registration was for — had the prediction not been written down, three passing
ladders would have made a fourth look like confirmation.

**Not explained**: why each insert becomes 3.65x more expensive between 18% and 26% generated share, with
retries and width both held. Contention between conflicting inserts on the same recent keys is the obvious
candidate; it would be the tenth mechanism proposed today and needs its own evidence, not this row. **Correction to my own reading**: I recorded that
share and rate were not separated by this batch. They are, by rows already in hand. Rate alone would need a
threshold at or below 25,000, since it fails there — but 18.1% *passes* at 50,000 and 80,000, up to **3.2x the
failing rate**, so rate alone is refuted. Share alone is refuted too, since 25.9% passes at 10,000. The failure
therefore needs **both** a share above ~18% and a rate above 10,000: a boundary that is a curve in
(share, rate), already bracketed on three sides.

That makes the operator's sentence more useful than "20% is safe and 30% breaks": **up to 20% the share is
bookkeeping at every rate measured, to 80,000; at 30% it is bookkeeping to 10,000 and fails by 25,000.** Two
numbers rather than one. What would tighten it further is 20% above 80,000, or 30% at an intermediate rate —
ds30's remaining 50,000 and 80,000 rungs only test whether the failure persists upward, which both readings
already predict, so they confirm rather than separate. The 50,000 and 80,000 rungs of ds30 will say whether the
tail keeps growing or plateaus, and `ds1`/`ds01`/`ds001`/`ds0001` bracket the other end of the axis.

Rungs 2 and up only: rung 1 of every ladder is measured while the table is still splitting — two tablets three
minutes after a bring-up — so its 408/409 ms belongs to a different layout and must not share a series with
the rest.

**The layout result that does hold, at one fixed workload.** Every row 5% double spends at gap 300,000, so the
pre-split is the only variable:

| tablets | committed | meets 1 s? |
|---|---|---|
| 8 | 172,260 | yes, at 239 ms |
| 88 | 27,400 | no |
| 96 | 20,800 | no |
| 120 | 20,300 | no |

A factor of 8.4 end to end, and only the smallest split meets the bound at any offered rate. The eight-tablet
row is a 90-second probe whose 300-second hold delivered ~125,000 at a censored p99, so the factor is bounded
below at 6; the other three are retirement rates under saturation, which hold whatever the queue depth. This is
what the document carries.

**For the record, none of the `split0` holds were quotable in either direction anyway, and the cause was the driver.** A hold
runs at the last *passing* probe, which after a seventeen-step climb is the top of the climb. ds10's hold
there no longer fit over 300 s — finished 96,545 against 102,770 offered — and the two step-downs then read
`finished` **113,273** and **135,455** against 95,157 and 88,108 offered, so they were draining hold 1's
backlog rather than measuring a rate. ds30's single `met=True` hold carries `inflight_growth` **−12,433/s**,
the same artefact with the sign that flatters it. No hold has been attempted *below* capacity on this series,
which is the experiment the figure needs: a fixed rate at roughly 75% of the passing probe, 300 s, fresh
deployment, pre-splitting off.

Two further cautions on those rows. `split0-ds20`'s 20,829 tps is not a low capacity — its search missed on
the first probe at 30,000 (4.05 s) and descended instead of climbing, which is the entire reason 20% reads
below both 10% and 30%. And every `split0` row is from 09-08 and 09-10, predating `fast_block_prepare` by
five days, so the series cannot share an axis with current numbers until it is re-run.

**Read p99 against the mean, always — it separates "a burst hit some transactions" from "everything was
slower", for free, from two columns already in every row.** This is the most reusable thing in this section and
it arrived last, after a day of treating single p99 values as measurements. A tail event moves p99 while the
mean barely follows; a uniform slowdown moves both together, and the *ratio* to the mean is then unchanged:

| | r1 p99 | r2 p99 | p99 x | r1 mean | r2 mean | mean x | reading |
|---|---|---|---|---|---|---|---|
| nosplit-ds5 | 408 ms | 192 ms | **2.12** | 148 ms | 138 ms | 1.07 | tail event |
| nosplit-ds10 | 409 ms | 150 ms | **2.73** | 146 ms | 133 ms | 1.10 | tail event |
| nosplit-ds20 | 227 ms | 195 ms | 1.16 | 163 ms | 140 ms | **1.17** | uniform shift |

`ds20`'s two ratios are 1.164 and 1.164 — identical to three digits — so nothing about the distribution's
shape changed between its rungs, while `ds5`'s went 2.76 to 1.39. The constant ratio is the argument, not its
value.

**It also sizes the event.** If a fraction *f* of transactions are delayed by roughly the p99 excess, the mean
excess is *f* times it, so *f* ≈ mean excess ÷ p99 excess (6f's): `ds5` gives 10/216 ≈ **4.6%** and `ds10`
13/259 ≈ **5.0%**, two independent ladders agreeing. The estimate self-checks, since it needs *f* above 1% for
p99 to sit inside the delayed group at all, and 5% clears that.

**One number both detects and sizes**, which is what makes it worth reaching for by default. Applied to
`ds20` the same formula returns *f* = 23/32 = **72%**: near unity means the whole distribution moved, which is
the uniform-shift case restated, so the test does not need a separate rule for "is there a tail at all". The
three ladders read 5%, 5% and 72% — and `ds20` is the **null control** the other two lacked, since it shows
what the test returns when nothing bursty happened. Note the separation: *f* distinguishes the two
regimes by more than an order of magnitude where the p99/mean ratio distinguished them by a factor of two,
from the same two columns.

**The statistic reports its own inapplicability**, which is a property worth stating rather than a caveat to
bury. *f* is a fraction only while the delayed group is a minority and p99 falls inside it; above roughly
20-30% it stops meaning "this share was affected" and starts meaning "the tail-event model does not apply
here". `ds20`'s 72% is the second reading, not a claim that 72% of its transactions were delayed.

What 5% of *transactions* localises is open, and the two readings differ in a way worth stating rather than
resolving. Temporally it is a burst covering some fifteen seconds of a three-hundred-second window — but
splitting spanned those whole windows, so that would need something brief *within* splitting rather than
splitting itself. Spatially it needs no burst at all: a transaction is delayed if it touches a tablet that is
mid-split, and one or two concurrent splits across twelve tablets is 8-17% of transactions, the right order
without any time localisation. The spatial one is the cheaper hypothesis and 6f now prefers it too: it follows from the tablet structure
rather than adding a brief sub-phase to a five-minute process, and it removes the tension between "5% of
transactions" and "splitting ran throughout" without an extra assumption.

**Pre-registered, and then corrected before the data landed — the rate it rests on was wrong.** The
registration said `ds30`'s rung 1 runs at 60,000, six times `ds20`'s, which would have made *f* a clean
discriminator: spatially *f* carries no rate term (concurrent splits ÷ tablet count) so it stays near 5%,
temporally it rises with the write rate. That was taken from a *description* of the plan. The definitions read
`[15000, 30000, 60000, 100000]` for all four shares, and the JSONL confirms `ds5` and `ds10` ran exactly that.

So **`ds30`'s rung 1 is at 15,000, rate-matched to `ds5` and `ds10`**, and both readings predict the same thing
there — same rate, same splitting. The test cannot discriminate them. What it does test is better than nothing
and different: whether the tail event **reproduces** at 15,000 across a third independent conflict share.

The revised registration, then: **`ds30` rung 1 should give *f* ≈ 5% under every reading currently alive.** An
*f* near 70% would mean the effect is not rate-linked either, and all three candidates are in trouble at once —
the most informative outcome available and the one to watch for.

It also sharpens the rate confound into something clean and testable, and the **superseded** `ds30` run
contributes to it even though it cannot serve the registration. The old rate list was 10,000 / 25,000 / 50,000 /
80,000 — verified directly from `ds20`'s four rows, which used it — so the old `ds30` rung 1 is at **10,000**,
not the 15,000 the registration needs. Which makes it a *second* ladder at 10,000:

| offered | ladders | *f* | reading |
|---|---|---|---|
| 10,000 | `ds20` | 72% | uniform shift |
| 10,000 | `ds30` (superseded run) | **registered: ~70%** | — |
| 15,000 | `ds5`, `ds10` | 4.6%, 5.0% | tail event |
| 15,000 | `ds30` (re-run) | **registered: ~5%** | — |

Two ladders at each rate, agreeing within their pair, would make the effect rate-linked with a threshold between
10,000 and 15,000 — and `tabhold12`'s rungs span exactly that gap with the layout pinned, so it tests it
directly. A disagreement inside either pair kills the rate story instead, which is equally useful. Registering
both before either lands, since the superseded run's row costs nothing to read and answers the half of the
question the re-run cannot.

**Registered, then falsified within the hour — and the question inverts.** `ds30`'s rung 1 landed at 10,000
(confirmed from its own row: finished 10,000, committed 7,414, abort 25.86%, p99 519 ms, mean 163 ms) and the
prediction of a uniform shift like `ds20`'s is wrong:

| ladder | share | rung 1 | p99 | mean | ratio | reading |
|---|---|---|---|---|---|---|
| ds5 | 5% | 15,000 | 408 ms | 148 ms | 2.76 | tail |
| ds10 | 10% | 15,000 | 409 ms | 146 ms | 2.80 | tail |
| ds20 | 20% | 10,000 | 227 ms | 163 ms | 1.39 | **no tail** |
| ds30 | 30% | 10,000 | **519 ms** | 163 ms | **3.18** | tail |

Two ladders at 10,000 disagree, 1.39 against 3.18, so **the effect is not rate-linked** — which is exactly the
outcome registered as killing the rate story, and it removes the threshold between 10,000 and 15,000 that
`tabhold12` was going to test. It is not share-linked either: 5%, 10% and 30% show tails and 20% does not, which
is not monotone. **So `ds20` is the outlier and three of four first rungs are the norm** — the question was never
"why do the first rungs have tails" but "why does one of them not".

**And splitting has the wrong sign for that pair** (e0's): `ds30` shows a 519 ms tail against `ds20`'s 227 ms at
the *same* offered rate while committing *less* data — 7,414 against 8,190 — so it fills slower and splits less.
Splitting predicts the smaller transient there, not one twice the size.

So the four tails read 407, 408, 227 and 519 ms, monotone in neither rate nor share, with the one mechanism that
had evidence now pointing the wrong way on the one pair that isolates it. At one rung each, the honest reading is
**noise rather than structure** — which is itself the argument for *excluding* rung 1 rather than explaining it.
That is where this stops: five candidate mechanisms have now been proposed for it across three sessions (cold
start, splitting, rate threshold, share, late-phase density) and none survives four ladders.

**Nothing downstream depends on the resolution**, which is why stopping is safe. The document's threats paragraph
claims only that the first rung after a bring-up is the weakest measurement in a ladder and that a single-rung
point carries that cost invisibly — true under noise as much as under any mechanism, and the reason the
size sweep's probes are flagged. The paired-ladder design remains the way to settle it if anyone wants to:
agreement *within* a pair at one rate is the thing noise will not produce.

**And the reproducibility problem underneath it, which is the more consequential half.** `ds20`'s observed rungs
were 10,000 / 25,000 / 50,000 / 80,000 while `ds5`'s and `ds10`'s ran 15,000-based ones. The first explanation
offered — that the file changed after `ds20`'s driver started — is **wrong**, and the log settles it: there is a
single `=== figures run:` header, at 12:00:21, with all four shares beneath it. One invocation, one load of
`EXPERIMENTS`, so all four ran from the same in-memory copy and the *per-share* rate difference was in that copy.
The harmonised `[15000, 30000, 60000, 100000]` now in the file is a later edit that **never described this batch
at all**. Two
consequences. `ds20` is not rate-comparable to its siblings, so the four-share series had one member measured on
a different ladder; e0 is re-running `ds20` and `ds30` under unified rates rather than papering over it in the
caption, which is right, because the two lists share no rate at all and there is no common-rate subset to fall
back on. And more generally, in the stronger form the log supports: **for a running batch the definition file is not
the record — the rows are.** Reading the current file produced a rate that no rung in this batch would ever
use, and it cost one wrong pre-registration. Anyone reconstructing this series later will make the same
mistake unless the rows are treated as authoritative. Same family as reading the array column instead of the
running count: an adjacent, plausible source that is not the one the measurement came from. Any claim about what an experiment measured has to come from the row's own `vars` and `limit`, not from
the current source — which is the same lesson as the `vars` check that caught the convoy, arriving from the
other direction.

Three of today's wrong readings would have been caught by this test alone: the censored p99s below, the
single-bucket ones, and a 3.35 s service time quoted from a p99 whose mean said otherwise.

**p99 clamps at 60,000 ms only; everything below it is bucket resolution.** The first version of this
paragraph called every repeated value a clamp — 60,000 ms on 114 rows, 29,900 on 26, 7,475 on 17, 44,850 on
10, 19,950 on 8, 14,950 on 8 — and that was too wide. The histogram's actual `le` set is 2 ms through 10 s,
then 15, 20, 30, 45 and 60 s, so 29,900 = 20,000 + 10,000 x 0.99 is exactly what `histogram_quantile` returns
by linear interpolation when **every** observation lands in the 20-30 s bucket. Coarse, but a measurement.

Only the 60,000 rows are clamps, where the quantile sits in `+Inf`, and the proof there stands: a ladder5m
rung reports p99 = 60,000 ms with a **mean of 69,334 ms**, and a mean above the 99th percentile is impossible
for any distribution.

The narrowing inverts what the wide claim implied about the eight-tablet holds. All six read 29,900 with means
of 25-26 s, so they are a **measured failure at sustained rates** rather than an unreadable number: eight
tablets moves from "unconfirmed" to refuted, and the 252 ms probe beside them is the anomaly needing an
explanation. Quote the mean in overload regardless — bucket resolution above ten seconds is coarser than any
claim worth making — and keep p99 for the region near the bound, where the buckets are milliseconds wide.

**Checked while there: the panels do not mix block producers within an x.** `fast_block_prepare` landed
mid-sweep, so the same experiment id has rows on both producers, and `best_per_x` takes the highest
throughput per x across configurations — which could have mixed them. After the reference-gap filter, panel
9c is one producer per x (x=0 on the old, 5–30 on the new) and 9a and 9b are entirely on the old one. The
cross-producer comparison inside 9c is sound anyway, since the producer change measured as a null result —
528,545 tps against 531,455 at 10,000-transaction blocks and 380,673 against 379,764 at 500 — and the conflict
points sit three orders of magnitude below any producer limit. No action; recorded so it is not re-checked.

The coarse set was the right trade and stays. Twenty-seven bounds against the role default's thousand equal
widths costs 27 Prometheus series per histogram instead of 1000, and the only thing it gives up is reading
exact tail values under overload — which is precisely what the day's work concluded nobody should do. A
saturated p99 tells you which decade you are in and the mean tells you the rest.

**The controlled comparison the tablet sweep could never produce, and it settles the mechanism.**
`9c-nosplit-ds5` rung 1 is a 300-second hold at the documented gap with pre-splitting disabled, and reading
the VC counters through its window against the 120-tablet numbers isolates the variable that the sweep kept
confounding:

| | keys/batch | attempts | insert | service time | µs per key |
|---|---|---|---|---|---|
| no pre-split | **682** | 1.91 | **0.016 s** | **31 ms** | 23 |
| 120 tablets | 350 | 1.91 | 1.720 s | 3.29 s | 4,914 |
| 96 tablets | 350 | 1.90 | 1.760 s | 3.34 s | 5,029 |

Same workload, same 4.88% abort share, and the **same 1.91 attempts per commit** — so the retry loop runs
just as often and the failure path is entered just as often. The batches are **1.95x wider**. And the insert
costs **108 times less**.

That resolves what the two-point fits could not:

- **Per-key is refuted outright.** The per-key cost is 209x apart between the rows, and the width moved in
  the *wrong direction* — wider batches, cheaper inserts. No monotone function of keys produces that.
- **Utilization cannot carry it.** Measured at fixed tablets, utilization is worth 20-55% (1.325 s draining
  against 2.050 s saturated at 96 tablets). It is not worth 10,800%.
- **The retry frequency is not the variable**, since 1.91 is identical across all three rows. Whatever the
  handler's `key = ANY(_keys)` and the rollback cost, they cost it per tablet touched.

So the failure path's cost is set by the tablet layout, and the earlier per-tablet *direction* survives even
though every numerical law fitted to it did not. It also confirms the service-time argument from its own
side rather than by absence: 31 ms of service against a one-second bound is why this configuration meets it,
where 3.29 s could not at any offered rate.

**Withdrawn: there is no tablet count at `pre_split_tablets: 0`.** I recorded 23, read from the yb-master's
`/api/v1/table?id=<uuid>`, as the layout of the no-split configuration. It is not a configuration value. The
setting creates **one** tablet and automatic splitting grows it from there, with each bring-up resetting it —
so `0` names a starting point, not a layout. A 30-second sampler over one ladder:

    12:35:27  ns_0   3 tablets   2 running
    12:35:57  ns_0   5 tablets   4 running
    12:36:27  ns_0   9 tablets   6 running

Two sessions published 23 and 10 for "the same configuration" and both were right at the moment they looked,
minutes apart on different tables. The gap between them has a second cause worth knowing: the API's `tablets`
array **includes mid-split tablets** (9 against 6 Running above), which `yb-admin list_tablets` filters. So
the Running count is the number to quote, and even that is a timestamp. `tablets.log` now records both every
30 seconds.

**The growth is a control rather than a confound, which is the useful part.** Over the no-split ladder the
offered rate rises 6.7-fold *and the tablet count grows about sixfold*, while the insert **falls** from 15.2
to 13.6 ms. So in this range the failure path is insensitive to the count as well as to the load — which is
why the ladder is quotable despite the layout moving under it, and it is stronger evidence than a pinned
layout would have been, because the count varied and the cost did not.

That reshapes the cliff rather than removing it: flat near 14 ms from one tablet to at least twenty, above a
second by 88, and the interval between is where the transition lives. On the settled count of **12** running,
12 to 88 is a 7.3x layout change for an 83x cost change, so linear per-tablet under-predicts by eleven and
ms per tablet spans 1.20 to 18.4 — a factor of fifteen. `tabhold{12,24,48,64}` with splitting pinned off are
what place it.

**Splitting settles at 12, and the expiry date is one future step at a nameable size.** I first wrote that
the count drifts continuously, having read the sampler's *array* column as the count. It is not: `ns_0`
running climbs 3 to 12 over five minutes after a bring-up and is then flat at 12 indefinitely, while the array
sits at 23 — twelve running plus eleven `Deleted` split parents awaiting the 60-second cleanup. So the layout
is **stable at 12**, which is what the flags say it should be:

    tablet_split_low_phase_shard_count_per_node    1        -> low phase ends at 12 tablets (128 MiB each)
    tablet_split_high_phase_shard_count_per_node   24       -> high phase runs to 288 (10 GiB each)
    tablet_force_split_threshold_bytes             100 GiB
    enable_automatic_tablet_splitting              true     (set nowhere in this repo)

At 12 tablets and 1.81 GB of SST — about 151 MB each — nothing qualifies for the high phase, whose threshold
is 10 GiB per tablet. The next split therefore needs roughly **120 GiB of table**, and then the high phase
carries the count toward 288 on twelve servers, which is exactly where §6 records a table created with 120
landing after eleven hours. So a no-split table has the same destination and merely starts further away, and
the durability question is the sharp form rather than the vague one: not "the ladder measured a moving target"
but "the layout is stable now and will step once, at a size we can name". The ladders write a few GB in twenty
minutes, so **no measurement here has ever entered the high phase**, and whether the 14 ms insert survives it
is unmeasured. The aged-table re-measurement is the experiment: one deployment, a soak to ~120 GiB, one
300-second hold at 60,000, insert compared against 13.7 ms.

**A third candidate for the rung-1 transient — proposed here, then tested against `ds20` inconclusively.** Rung 1 of `9c-nosplit-ds10` ran
from roughly 12:35 to 12:40, and the count climbed 3 to 12 across exactly that window; every later rung ran at
a stable 12. So rung 1 is measured *while the table is actively splitting*, which costs real work and points
the right way — its insert is 15.2 ms against 13.6 and its p99 407 ms against 190. `ds20`'s rung 1 looked like a refutation
— it splits just as visibly, running 1 to 6 with a 7→5 dip inside the window, and shows p99 227 ms against a
163 ms mean, a ratio of 1.39 where the other two read 2.8 — and it is not one. **The comparison is
confounded**, which 6f caught: splitting is size-triggered, so `ds20`'s lower offered rate (10,000 against
15,000) is *why* its split progress reached only 6 of 12. Less rate and less splitting are not independent
variables here, and a smaller tail under both is what the splitting hypothesis predicts rather than what
contradicts it. I read a dose-response as a counterexample.

`ds20`'s rung 2 then landed at 195 ms — between the two outcomes pre-registered for it — and **decomposing
p99 against the mean separates the ladders qualitatively rather than by degree**:

| ladder | r1 p99 | r2 p99 | p99 x | r1 mean | r2 mean | mean x |
|---|---|---|---|---|---|---|
| ds5 | 408 ms | 192 ms | **2.12** | 148 ms | 138 ms | 1.07 |
| ds10 | 409 ms | 150 ms | **2.73** | 146 ms | 133 ms | 1.10 |
| ds20 | 227 ms | 195 ms | 1.16 | 163 ms | 140 ms | 1.17 |

`ds5` and `ds10`'s first rungs are **tail events**: p99 doubles or triples while the mean moves 7-10%, so a
minority of transactions were hit hard. `ds20`'s moves p99 and the mean by the *same* 16%, which is not a tail
signature but a uniformly slower rung, and a lower offered rate accounts for that with no transient at all.
The ratio says it compactly: 1.39 at *both* of `ds20`'s rungs means the distribution's shape did not change
between them, where `ds5` goes 2.76 to 1.39.

**The decomposition is worth keeping as a test in its own right.** It separates "a burst hit some
transactions" from "everything was slower", for free, from two numbers already in every row — and this section
has been reading single p99 values all day where that distinction was available.

It aligns with split progress across all three ladders: `ds5` and `ds10` reached the resting 12 inside their
rung-1 windows and show the tail event, `ds20` was at 6 of 12 and shows none. Three points and a qualitative
difference rather than a magnitude fit, so the late-phase version of the mechanism — cost concentrated where
splits are dense — is recorded as supported rather than withheld.

**The rate confound is untouched and still decisive**, so this is support and not a settlement: both
tail-event rungs are at 15,000 and `ds20`'s at 10,000, so "15,000 produces a tail and 10,000 does not" remains
available however arbitrary it looks. Three candidates stand, with splitting now ahead on evidence rather than
on assertion. `tabhold12` separates them exactly — pinned layout, splitting off, rungs at both rates — so a
pinned 15,000 rung with a 2.x p99/mean ratio means rate, and 1.4 means splitting.

`tabhold12` with splitting pinned off remains the discriminator, and it is now a better one than when it was
queued: its rungs sit at 10,000-15,000, exactly the range in question, so it separates rate from splitting as
well as splitting from warm-up.

**Withdrawn: `tx_status` had not settled when I looked, and has since.** Both tables read 23 array / 12
running from 12:44 onward. And the setting covers more than two: `${SPLIT_INTO_TABLETS}` appears in
`utils/statedb/create_namespace_tmpl.sql:22` and in `init_database_tmpl.sql` at **both 13 and 28**, so three
tables are pre-split by it and `tabhold12` pins all of them.

**A running count can fall, and the dips are an instrument.** `tx_status` went 3→2, 4→3 and 8→7 on
consecutive samples. Not a merge — YugabyteDB has none — presumably a parent leaving `Running` before both
children enter it, or an inconsistent snapshot. Either way a dip means **splitting is active right now**,
which turns the count from a label into a live marker of the concurrent workload. Two dips fall inside
`9c-nosplit-ds10`'s rung-1 window, at 12:36:27 and 12:37:27, so the splitting hypothesis has direct evidence
from that rung rather than an inference from the count having climbed across it — and an instrument that does
not depend on `tabhold12` returning.

**What a soak would cost, measured rather than guessed.** With SST size in the sampler, `ns_0` grows
**0.049 GB per tablet per minute** at 56,000 tps — **0.0146 GB per tablet per million transactions**. From 0.4
to the 10 GiB high-phase threshold is 9.6 GB per tablet:

| offered | time to the next split |
|---|---|
| 56,000 | 196 min |
| 150,000 | 73 min |
| 250,000 | 44 min |
| 350,000 | **31 min** |

The per-transaction restatement above was wrong by a factor of sixty on first writing — divided by
transactions a *second* rather than by transactions a *minute* — which e0 caught. The **time** column is
unaffected, since it scales the GB-per-minute slope by the rate ratio and is dimensionally right; only the
per-transaction figure was off, and it is the one someone else would reuse. A second slip in the same check
multiplied by the tablet count when the per-tablet slope already accounts for the split, giving 8,229M
transactions against the true ~685M. Both are recorded because the arithmetic here is load-bearing: the same
column decides how long a soak runs and whether it fits the volume.

**And the binding constraint turns out to be disk, not time**, which rules out the obvious hedge. Crossing
needs ~685M transactions and about 180 GB of ledger, against 514 GB free on the tightest volume — comfortable.
Padding the hold to 125 minutes to cover the pessimistic 95,000 tps case would write about 2.6 billion
transactions and ~690 GB, so the run would die on the disk floor having proved nothing: a **fourth** way to
get an uninformative null, on top of the three already found here. Two bounded soaks with the count read
between them is therefore strictly better than one long one, and that is what is queued.

This matters because the aging test queued as `ds5-hi-age` runs after three 300-second rungs — fifteen minutes
of load at an average 250,000, reaching about **3.3 GB per tablet, a third of the threshold**. The count stays
12, no split happens, and the measurement reads "still 14 ms" while meaning "no split occurred". That is the
same false reassurance the `SKIP_DEPLOY` guard was built to prevent, one layer up. The fix is to gate the soak
on the observation rather than on a duration: `tablets.log` reports the running count and GB per tablet every
30 seconds, so a soak can run until the count steps off 12 and hold the comparison rung only then.

**Retracted twenty minutes after recording it: 30% does not break — the 25,000 miss is an isolated
anomaly.** `ds30`'s third rung landed at 50,000 and **met**, with everything back to normal:

| offered | p99 | mean | p50 | insert | attempts | growth | met |
|---|---|---|---|---|---|---|---|
| 10,000 | 519 ms | 163 | 164 | 14.5 ms | 1.97 | 0 | yes |
| 25,000 | **3,565 ms** | 258 | 130 | **52.9 ms** | 1.96 | 0 | **no** |
| 50,000 | **199 ms** | 150 | 143 | **11.7 ms** | 1.95 | −6,800 | yes |

So the failing rung is bracketed above and below by passing ones, and its insert of 52.9 ms is 4.5x both
neighbours (14.5 and 11.7). A knee does not do that. Whatever the 25,000 rung was, it was a transient confined
to one window — the same profile as the rung-1 tails, which is a fourth reason not to have built a bound on a
single rung. 6f had already asked for a 25,000 repeat in the re-run on exactly this ground, before the 50,000
rung landed.

Note the growth column: −6,800/s at 50,000. The redeploy-on-miss fires after a failing rung, so that rung began
on a fresh deployment, and a negative reading there is most likely the in-flight count settling from the health
check rather than a real drain. Worth flagging because `met` gates on `finished ≤ offered` and on nothing about
*negative* growth, so a settling window passes the gate — which is harmless here but is the mirror image of the
draining-window problem the gate was built for.

*(Superseded, kept for the retraction:)* **The conflict share is bookkeeping to 20%, and 30% breaks.** The first version of this entry said the share
"barely matters", on 5% and 10% alone. 20% then held to 80,000 and **30% missed the bound at 25,000**, so the
claim needs its bound:

| share | every rung met to | p99 | busiest CPU |
|---|---|---|---|
| 5% | 100,000 offered | 190 ms | 11% |
| 10% | 100,000 | 187 ms | 11% |
| 20% | 80,000 | 193 ms | 10% |
| 30% | 50,000, with an isolated miss at 25,000 | 199 ms (3,565 at the miss) | 7% |

**The boundary cannot be placed from this data.** It is either between 20% and 30% in share, or between 10,000
and 25,000 in rate at 30%, and nothing here separates them; ds20's top rung was 80,000 and ds30's second was
25,000, so the two ladders never meet. The re-run brackets it at 15,000 and 30,000, which is within a factor of
two — enough to state where it is without a finer ladder.

**The miss is a heavy tail, not saturation.** At 25,000 the rate arrived in full (finished 25,091), nothing
queued, CPU was 3%, and the **median improved** — 164 ms to 130 — while the mean rose to 258 and p99 to 3,565.
*f* against rung 1 as baseline is 95/3046 ≈ **3.1%**: about three transactions in a hundred delayed by some
three seconds.

**And `db_insert` rose 3.65x at constant attempts and constant width** — 14.5 ms to 52.9, with attempts per
commit 1.972 → 1.962 and keys per call 234 → 226. So each failing lookup got *more expensive*, not more
frequent. This is 6f's observation and it is the first time today that cost moved with **neither the layout nor
the batch geometry**; every previous move was one of those two. A third axis, appearing only at the top of the
share range, where nothing had been measured before. Same-key contention between concurrent failing inserts is
the obvious candidate and is **not** adopted — it would be the tenth mechanism proposed today and needs its own
evidence rather than a plausible story. Compare `ds20` at 50,000 and 80,000: insert 11.6 and 11.9 ms, p99 193
and 192 — no such effect at the next share down.

**Kept separate from the rung-1 tails, on 6f's discrimination.** Rung 2's 3,565 ms sits 28% into its bucket and
its mean moved 58%, so it has real mass; rung 1's 519 ms sits 7.8% into its bucket with the mean unmoved, so it
may still be a quantile-placement artefact. The two are different in kind and folding them together would
launder a solid result into an unresolved one.

**What survives, and is the strongest claim here — corrected.** I first wrote this as "every rung after the
first, across all four shares … twelve rungs". Both numbers were wrong, and the error was the bad kind: `ds30`'s
rung 2 **is** a rung after the first, and it read 3,565 ms, so stating four shares put the one contradicting
measurement inside a range constructed to exclude it. 6f caught it.

Counted properly: of the ten rungs after a first rung, **nine met and one missed**. The nine span **three**
shares — 5%, 10% and 20% — at offered rates from 25,000 to 100,000, and read p99 **150-195 ms**, mean 132-140,
p50 125-128, ratio 1.13-1.40. So **latency is independent of conflict share and of rate across that region**,
and at 30% the independence ends somewhere between 10,000 and 25,000.

The exclusion is the finding rather than a blemish on it: three flat shares and one that breaks is a cliff, and
it is why the panel's 30% bar is ~10,000 against ~100,000 for the others. Stated as four shares it would have
been a range with a counterexample inside it — which is the same failure as quoting a p99 whose own mean
contradicts it, one level up.

*(Superseded framing, kept for the retraction:)* **The conflict share barely matters once the pre-split is
off.** Both no-split ladders ran four
300-second rungs to 100,000 offered and every rung met the bound:

| double spends | top committed | p99 | busiest CPU |
|---|---|---|---|
| 5% | 95,122 | 190 ms | 11% |
| 10% | 90,491 | 187 ms | 11% |

A 5% shortfall for twice the double-spend rate, against a 25x collapse at the 120-way split. That is a
stronger form of the result than either share alone, and a near-flat series across shares is the shape the
published 9c panel has — the first time anything measured here has reproduced it.

**The delivered conflict share falls short of the configured one, and the gap grows** (e0's observation,
confirmed from the rows):

| configured | delivered | ratio | gap |
|---|---|---|---|
| 5% | 4.88% | 0.975 | 0.12 pts |
| 10% | 9.51% | 0.951 | 0.49 pts |
| 20% | 18.10% | 0.905 | 1.90 pts |
| 30% | 25.86% | 0.862 | 4.14 pts |

The direction matters more than the size: the axis **overstates** the conflict rate at high shares, so it
*understates* the pipeline's tolerance rather than flattering it. A plausible mechanism is that a conflicting
transaction is itself aborted, so the keys it would have created never commit, and a later reference naming one
of them finds nothing to collide with — the reference pool is diluted by the abort rate it produces.

The three ratios happen to fit `c(1 - c/2)` to three digits, and that is **not** recorded as a law: three
points, and nothing accounts for the factor of two. It would be the sixth closed form proposed on one batch of
points in this section.

For the figure: both series plot the *configured* share, since `r["x"]` comes from the experiment definition and
`measured_conflicts` is used only in the corner notes, while the hatched rejected portion of each bar is the
*delivered* share. The two differ by 30% at the top of the sweep, so the axis label has to say which it is.

**A pin written for one experiment silently invalidated another that reused its vars.** The durability test
was queued carrying `--enable_automatic_tablet_splitting=false`, inherited from the `tabhold` points where
pinning is the whole point. So a test whose *subject* is splitting could not split, and would have returned
"still 14 ms" from a table that was never going to move. This is the same fault class as the drain rate that
matched capacity and the `FX_ONLY` prefix that matched nothing: **an instrument that answers a different
question than the one asked, and reports success either way.** The rule it adds to the three-clause one is
narrow and worth stating: when a test's subject is a mechanism, re-read the flags that disable that mechanism
before reusing a vars block that was written for a different point.

It was caught by arithmetic rather than by inspection, which is the reusable part: checking whether the soak
was *long enough* is what surfaced that it could never have worked at any length. A sizing calculation is a
cheap way to find an impossible experiment, because an impossible one usually fails the sizing too.

**And if splitting is what contaminates a first rung, the contamination is general.** Every first rung on a
fresh deployment is suspect, not only these — which includes the single-rung points in the size sweep, where
there is no second rung to compare against, so the contamination is invisible rather than merely present.
That is 6f's, and it survives the change of mechanism from cold start to splitting.

My first form of the rung-1 argument was circular and 6f caught it: I argued that fewer tablets should make
the failure path cheaper, so rung 1 being *more* expensive rules the count out — but the reason to believe the
cost is insensitive in that range is precisely that the count rose while the cost fell, so monotonicity cannot
then be assumed as a premise. The non-circular form: between about 2 and 23 tablets the cost does not track
the count in either direction, therefore rung 1's excess is not a count effect and cold start is what remains.

I also over-read the confound when reporting it, and the correction is worth recording next to the claim:
having found the count moving, I told the driver the four conflict shares might not share a layout and
suggested pinning splitting off for the remaining ladders. The insert's insensitivity above already answers
most of that — the shares can differ in count by a factor of several without the comparison moving, so the
per-share counts are worth reporting but were not worth changing the plan for.

**Unexplained, and flagged rather than fitted:** the batches are wider at no pre-split (682 keys against
350) even though the downstream is 108x faster, which is backwards for a batcher whose only floor is
`MinTransactionBatchSize: 1` and which should therefore accumulate *less* per send cycle when the service
below it is quick. Five mechanisms have already been retracted in this section; this one is recorded as an
observation.

**What the 5M ladder shows is capacity, and nothing about load.** This claim was wrong three ways before
it was right, so the sequence is recorded rather than just the conclusion: first the throughputs were
withdrawn along with the latencies, then reinstated as evidence of rate-independence across a 16x range of
offered rate, and only then checked against what the generator actually sent:

| rung limit | 25,000 | 50,000 | 100,000 | 200,000 | 300,000 | 400,000 |
|---|---|---|---|---|---|---|
| generator **sent** | 24,909 | 21,636 | 21,455 | 21,455 | 21,091 | 21,091 |
| % of the requested rate | 99.6 | 43.3 | 21.5 | 10.7 | 7.0 | **5.3** |

**The applied load never varied.** The sidecar's window holds the generator near 21,000 whatever rate is
requested, so the ladder measured one applied rate six times. The `grow` field said so all along and was
read past: at 400,000 against a ~20,000 capacity the backlog would have to grow ~380,000/s, and it reads
between +4,100/s and −100/s. So the rungs establish that the pipeline cannot be driven past its capacity —
true of any saturated system — and **not** that capacity is independent of load. Rate-independence is
therefore unmeasured, not established.

What the rungs do give is capacity, and the VC's own counters give it far more tightly than the generator's
status arrivals do: **20,617 / 20,501 / 20,445 / 20,362 / 20,182 / 20,226**, flat within 2.1% for a capacity
of **20,400**. The generator's figures for the same rungs spread ±17% (CV 12.9%, 1.5x this cluster's 8.8%
repeatability) because it counts status arrivals with millions of transactions queued ahead of them — the
noise was the instrument's, not the system's. Every mean and p99 from these rungs stays withdrawn.

**One 90-second probe met the SLO at 8 tablets, and no 300-second hold has reproduced it.** The row:

    [9c-ds5-split8] probe limit=181,031 finished=181,091 committed=172,260 abort=8,831
                    p99=239ms mean=171ms grow=0/s app=6% cpu=21% -> MET

172,260 tps committed at **239 ms p99**, 171 ms mean, 21% CPU, zero in-flight growth — comfortably inside
the one-second bound, on a conflicting workload. It appears twice in the logs, at 19:41 and 01:23,
identical to the digit, which makes it one measurement recorded twice rather than two agreeing runs.

The three holds that followed, offered 155,204–181,031, returned 122,798–130,233 committed at 25–26 s
means. By this project's own discipline that leaves the rate **unconfirmed, not disproven** — and the
likely reason is the one recorded in §8: each of those holds followed a probe above the knee and inherited
its queue.

So no figure is drawn from these rows. A probe that meets against holds that miss, and a contaminated line
against a saturated one, invites a reader to read measurement state as architecture; an axis raised to fit
25-second means would present them as operating points. The figure waits for ladders whose drains converge
— `FX_DRAIN_RATE` well under capacity — at both tablet counts. Those either confirm 172,260 at 239 ms on a
fresh deployment or show the probe was the artefact, and both outcomes are worth reporting. The fix is either `FX_DRAIN_RATE` well under capacity
(2,000 here: 18,000/s net clears 3.3M in ~185 s, inside the four 60 s rounds and above the
`4 * DRAIN_RATE` floor) or, durably, a drain that parks at a fraction of the last measured throughput so
no future workload can land on the default. The counter ratios above are unaffected: a backlog changes
neither the keys in a batch nor the reads that batch provokes.

## Audit results, so they are not re-run

**Every quantitative claim in the evaluation's abstract, against `figures-orderer.jsonl` — all clean.**
499,091 tps at a 447 ms median and 658 ms p99 is `e2e-shape-4s-hi` at 500,000 offered on a **300 s window**,
fully retired with zero aborts and growth +133/s. Every rate below it arrived within 0.2%: 350,000 → 350,000,
400,000 → 399,636, 450,000 → 450,182. Eight shards reach 500,000 against four shards' 499,091, 0.18% apart,
and four shards hold the lower p99 at both matched rates — 658 against 754 ms at 500,000 and 547 against
684 ms at 450,000. The busiest machine is `commit6` (four shards) and `commit5` (eight) at the top of each
ladder and a router below it, which is what "the busiest machine is a validator--committer rather than any
ordering component" rests on. Behind "no machine anywhere exceeds 82%" the true maxima are 80.5% and 81.6%,
and the load generator reads 76.7% against a quoted 77%. And the 250,000 tps small-batch ceiling reads
`blk_rate` **499.98** on `e2e-curve-small`, so "a block-rate limit near 500 blocks a second" is measured
rather than inferred.

**No plotted point in either arm mixes configurations within one x**, checked field by field across `vars`.
On the committer arm this holds only after the reference-gap filter: panel 9c was drawing gap-1,000 rows at
x=5 and x=10 alongside gap-300,000 rows at x=20 and x=30. Panel 9c is now one block producer per x — the old
producer at x=0, the new one at 5 through 30 — and 9a and 9b are entirely on the old one; the cross-producer
comparison inside 9c is sound because the producer change measured as a null result. On the orderer arm no
field varies within any plotted point at all.

**`curve500buf` does not contaminate figure 2.** It carries its own `figure` id, so the deeper-buffer ladder
is not pooled with `curve500`. Its knee is 430,145 tps sustained over 300 s at a **209 ms median, 218 ms mean
and 408 ms p99**, with the load generator at 21% and the busiest machine at 75%; 480,000 offered collapses to
433,727 at 7,181 ms. Quote the median with its statistic named, since the p99 there is nearly double it.

## What `db_insert` actually spans, from the source

`databaseTxBatchCommitInsertNewKeyWithValueLatencySeconds` wraps `insertStates` only
(`service/vc/database.go:381`), so it covers three things: the blind `INSERT`, the **plpgsql savepoint
rollback** — `insert_ns_*` has an `EXCEPTION WHEN unique_violation` block, so the failed statement is undone
before the handler runs — and the handler's `key = ANY(_keys)` lookup.

It does **not** cover the outer distributed transaction's rollback: `commit()` returns early on `res != nil`
and its deferred `rollBackFunc` fires *after* `insertStates` has returned. Nor does it cover the retry loop in
`commitTransactions`. **So the true cost per commit is higher than `attempts x db_insert`, not lower**, and
every figure derived from that product is a lower bound. It is also why `tx_per_insert` reports width divided
by attempts: there is one observation per attempt, not per commit.

The conflict-free case is the control that puts the cost on the failure path rather than on the write itself —
the same `INSERT` to the same ~120 tablets sustains 518,000 tps when nothing violates. That leaves the
savepoint rollback and the handler's lookup as the two candidates, and **no measurement yet separates them**.
`hold8` is the experiment that can: at 8 tablets there are ~38 keys per tablet against ~2.5 at 120, so a flat
per-tablet cost predicts 0.11 s while a cost growing with keys-per-tablet predicts more.

## Two loose ends with no owner

**`chunk64`.** Sixty-four transactions is ~128 keys, so 128 x 120 = 15,360 sits *under* the batching threshold
and the point should have been fast. It gave 46,182 tps. `vcservice_batcher_input_queue_size` exists, so the
validator--committer probably re-batches and the dependency-graph chunk does not control the insert's key
count at all. One `EXPLAIN (ANALYZE, DIST)` at the real batch width, reading `Storage Read Requests`, settles
it.

**`SimpleManager.depFreeTxBatches` has no gauge.** About 497,000 transactions are dependency-free, released,
and in none of the seven queues that are instrumented, so they can only be in that slice — which the code
itself calls "deliberately unbounded" and which nothing measures.

