<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Cluster Optimization Log

A record of the changes that took a nineteen-machine deployment from 80,000 to roughly 357,000
committed transactions per second, what the evidence for each was, and which of them turned out
to buy nothing. It is a companion to the [Performance Tuning Guide](performance-tuning.md): that
guide says what each parameter does, this one says what actually moved on real hardware and how
the constraint was located each time.

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
| **Simple manager, `waiting-txs-limit` 500,000** | **486,941** | 1,099 ms | 1,988 ms | 786 MB |

Two results are in that table and they are worth separating.

**The manager choice is worth 47.6%** — 486,941 against 329,854 at an otherwise identical
configuration. The row shows why: under the default manager, database batch commit falls to 62 ms
and the commit machines to 60% CPU, from 140 ms and 76%. The database is starved while the graph
sits full. Section 8 records why an earlier version of this comparison, which put the gain at 41%,
was not valid.

**The waiting limit is a latency choice, not a throughput one.** In-flight tracks whatever ceiling
the limit sets — 2,039,200 against a 2,000,000 limit — and 470,815 tps × 4.33 s reproduces that to
three digits, so above what is needed to keep the validator-committers busy the surplus is pure
queueing delay at about 4 KB of coordinator memory each. Memory is exactly linear: 1.0 / 8.2 / 79 GB
at 200,000 / 2,000,000 / 20,000,000. 500,000 is the throughput maximum and 200,000 gives up 3.5% of
it for half the mean latency and a third of the p99. 20,000,000 was simply a mistake: it bought
nothing, and cost 100x the memory of the setting that beats it.

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

## 5. The load generator became the limit

From roughly 325,000 tps onward, most measurements were of the harness rather than the committer.
This is the single most important caveat on every number in this document.

**ECDSA nonce generation.** Go's ECDSA draws a fresh nonce per signature through `getrandom(2)`,
and the cluster's kernel has no vDSO for it, so that is a real syscall per signature — about
300,000 per second. A goroutine dump showed 38 of 95 goroutines parked in an identical stack ending
in `syscall.Syscall`, and the machine sat at 51% CPU because the signing workers were blocked in
the kernel rather than computing. Nothing downstream was saturated and every queue was empty.

Switching the namespace policy to `EDDSA` removed it: Ed25519 signing is deterministic per
RFC 8032 and reads no entropy, so the count of goroutines in `getrandom` went to zero and the peak
went 325,600 → 374,400.

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

## 6. Where the constraint is now

The database commit path, at roughly 487,000 tps, but only once the coordinator's dependency graph
stops being it. With the default graph manager the constraint is the coordinator and the database
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

### 6.2 What the load generator can offer

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
| Load generator ECDSA `getrandom` | removed (Ed25519) |
| Coordinator dependency graph mutex | removable, no throughput gain |
| Database SQL front-end concentration | fixed, no throughput gain |
| Sidecar channel buffering (latency only) | reduced 6× |
| Load generator signing throughput | removed (Ed25519, `gen-batch`) |
| Sidecar block delivery | shown never to have been the constraint |
| Coordinator dependency graph halting under load | fixed (`drain_test.go`) |
| Oversized `waiting-txs-limit` (latency and memory) | 20M → 500K, 100× less memory |
| **Database commit path** | **current** |

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
each alone was misleading. The dump found the `getrandom` blocking that no CPU measurement would
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

**A benchmark that hangs looks like a benchmark that is slow.** `BenchmarkDependencyGraph` numbered
its batches from 0, and the local dependency constructor releases a batch only after its predecessor,
so every default-manager case waited forever on a predecessor that could not exist. It had never
measured the production manager at all — only the simple manager, which does not use that
constructor. What made it look like slowness rather than a deadlock is that the benchmark generates
`b.N*3` transactions with a single-worker profile, so at large `b.N` it genuinely does spend minutes
generating. A goroutine dump distinguished the two in one step: 1.1% CPU, and the constructor parked
in `sync.Cond.Wait` for five minutes. With it fixed, the local constructor pool turns out not to
bound the default manager at all — 1 through 32 constructors give 216,886 / 249,691 / 220,000 /
246,929 / 221,484 / 228,068 tx/s, no trend — because the ceiling is in the global manager's two
single goroutines.

**The dependency graph has not been measured on the work it exists for.** Every figure in this
document was taken with `key-backref-rate` at 0, where each transaction's two read-write slots get
fresh unique keys. No two transactions ever touch the same key, so the graph tracks transactions
that cannot conflict and the MVCC validator never aborts one. Both managers were compared on a
workload that gives the graph nothing to do, and the simple manager's advantage may not survive a
workload that does.
