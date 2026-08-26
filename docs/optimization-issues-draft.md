<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->

# Optimization issues: draft

Drafts for the issues to open for the work in `optimization-summary.md`. Nothing here is filed yet.

One umbrella issue plus a child per change. Evidence for every number is in
`cluster-optimization-log.md`; the issue bodies below state only the change and what drove it.

Negative results are not filed as issues. The one that concerns an existing issue —
`pgx.Batch` (**#307**), worth roughly 0.4% here because a batch still executes sequentially
server-side — is recorded as a comment on #307 and is deliberately **not** a child of the umbrella.

Deployment and configuration tuning is deliberately **not** filed — it is specific to this
evaluation's nineteen-machine cluster, and a shipped default would have to be argued on other
hardware. See section 4 of the summary.

## The issues

| # | Title | Repo | Status |
|---|---|---|---|
| U | Committer throughput and latency: findings from the nineteen-machine evaluation | committer | umbrella, new |
| 1 | [sidecar] Make the block store transaction ID index optional | committer | new |
| 2 | [sidecar] The relay tracks in-flight blocks and TX IDs in sync maps on its per-TX path | committer | **#772, already open** |
| 3 | [coordinator] Allow selecting the simple dependency graph manager | committer | new |
| 4 | [coordinator] The simple dependency graph can latch the pipeline under load | committer | new |
| 5 | [sidecar] Parse a block's transactions in parallel | committer | new |
| 6 | [sidecar] Key validation and TX references allocate per transaction | committer | new |
| 7 | [sidecar] Back a block's decoded transactions with one allocation | committer | new |
| 8 | [sidecar] Mapping's result carries the scaffolding that built it | committer | new |
| 9 | [grpc] Add a per-client and per-server `flow-control` section; HTTP/2 windows are unset and cap the pipeline | committer | new |
| 10 | Benchmarks for attributing committer performance | committer | new, parent |
| 10.1 | [sidecar] Benchmark the whole service end to end | committer | new, child of 10 |
| 10.2 | [coordinator] Benchmark the signature verifier manager | committer | new, child of 10 |
| 10.3 | [loadgen] Benchmark the submit path | committer | new, child of 10 |
| 10.4 | [loadgen] Sweep transaction generation over core count | committer | new, child of 10 |
| 10.5 | [coordinator] The dependency graph benchmark cannot run the default manager | committer | new, child of 10 |
| 11 | [blkstorage] Do not build tx index information no index will read | **fabric-x-common** | new, filed there |

---

## U. Committer throughput and latency: findings from the nineteen-machine evaluation

An evaluation on nineteen machines — one sidecar, one coordinator, three signature verifiers, six
validator-committers and a twelve-node YugabyteDB — took the committer from 80,000 to 500,000
transactions per second sustained, and identified where the remaining constraint is.

This issue collects the changes that produced it. Each child is independently reviewable; the
measurement that motivated each one is in its own issue, and the full account with all the evidence,
including the retracted findings, is in `docs/cluster-optimization-log.md` and
`docs/optimization-summary.md`.

Children: #1 #2 (#772) #3 #4 #5 #6 #7 #8 #9 #10 (itself the parent of the five benchmark issues),
plus one in `fabric-x-common` for the block store's unused transaction index information.

Where the constraint ends up: not in the committer. At 500,000 tps every committer stage has headroom
— sidecar 14% CPU, coordinator 21%, verifiers 34%, validator-committer preparer 4 of 96 workers busy —
while the database runs 83% CPU and 88% disk busy. Raising throughput further means writing fewer
bytes per transaction, not tuning the committer.

## 1. [sidecar] Make the block store transaction ID index optional

The block store indexes `IndexableAttrTxID`, which writes one LevelDB entry **per transaction** rather
than per block. A 20-second CPU profile of a saturated sidecar attributed 35% of its samples to
goleveldb compaction, all of it maintaining that index, which had grown to 33 GB against 118 GB of
blocks.

The signature is throughput that decays as the ledger grows rather than holding steady: committed fell
from 102,102 to 67,886 tps over two hours at a fixed offered rate, while bytes written per transaction
rose from 1.89 to 2.70 KB.

Add a setting to drop the index for deployments that serve neither `GetBlockByTxID` nor `GetTxByID`.
Worth 115,200 → 297,200 tps. The index selects the block store's on-disk format, so it can only change
on an empty ledger.

## 2. [sidecar] The relay tracks in-flight blocks and TX IDs in sync maps on its per-TX path

Already open as **#772**. No new issue.

## 3. [coordinator] Allow selecting the simple dependency graph manager

The default dependency graph manager becomes the pipeline's constraint before the database does: at an
otherwise identical configuration it commits 329,854 tps against the simple manager's 486,941, a
difference of 47.6%. The reason is visible downstream — under the default manager database batch commit
falls to 62 ms and the commit machines to 60% CPU, so the database is starved while the graph sits full.

Add a setting to select the simple manager. The two managers implement the **same** dependency
relation, so this is an implementation choice and not a semantic one:

- Both derive their keys from the same `readAndWriteKeys` (`transaction_node.go`), including the
  meta-namespace read key that ties a namespace lifecycle transaction to the transactions in that
  namespace.
- Both track the same coarse-grained relation: two transactions that share a key conflict unless both
  only read it. In the default manager that is `dependencyDetector.getDependenciesOf`, which copies
  `writeOnly` and `readWrite` for a read key and all three maps for a write key; in the simple manager
  it is `waiting.add`, where a reader joins a running group of readers and anything else queues behind
  it. The default manager's own comment says it deliberately tracks only the coarse-grained relation.
- `TestDependencyGraphManager` already asserts identical dependency behaviour against both.

What differs is the data structure and the concurrency: a DAG of per-transaction dependency sets over
three key-to-transaction-set maps, built by a worker pool and merged under one mutex, against one
key-to-FIFO map owned by a single goroutine. Where the default manager keeps a map entry per waiting
transaction per key, the simple manager keeps a counter — and every transaction reads the
meta-namespace key, so that entry is the whole waiting set, inserted and deleted per transaction.

Left as a setting rather than a default change for two reasons, neither of them about which
dependencies are tracked: the 47.6% was measured with `key-backref-rate` at 0, so no two transactions
touched the same key, which is the case that favours a single goroutine; and the simple manager has an
unfixed defect (#4).

## 4. [coordinator] The simple dependency graph can latch the pipeline under load

Under sustained load the simple manager can stop releasing transactions and never resume. Everything
downstream continues to look healthy — queues drain, no errors are logged — so it presents as a hang
rather than a failure.

A third defect, found while re-validating the claim above and **not** yet fixed: the simple manager can
make a transaction wait on itself. `readAndWriteKeys` adds the composite key `_meta:<ns>` as a
reads-only key for every non-system namespace in a transaction, and a transaction that updates that
namespace's policy through `_meta` produces the *same* composite key as a reads-and-writes key.
`processTxBatch` then calls `checkTXFree` for that key twice, once as a writer and once as a reader;
the second call queues the transaction behind the running group the first call created, which is the
transaction itself. It is never released and the key is never freed. The default manager cannot hit
this, because `getDependenciesOf` runs before `addWaitingTx`, so a transaction is never in the
detector when its own dependencies are computed.

Reproduced with one transaction carrying namespaces `_meta` (read-write on key `ns1`) and `ns1` (a
blind write): the default manager releases it, the simple manager releases nothing. Per-namespace
duplicate-key validation does not catch it, since the two contributions come from different
namespaces.

Prerequisite for #3 rather than a gain in itself: the manager cannot be recommended until these are
fixed. A regression test per defect belongs with the fix.

## 5. [sidecar] Parse a block's transactions in parallel

`mapBlock` parses and validates a block's messages one at a time on a single goroutine, and mapping is
the sidecar's largest per-block stage.

Parse across up to 16 goroutines, then fold the results into the block in message order so that the
transaction-ID dedup set and the batch order stay single-threaded and the outcome does not depend on
how the parsing was split. Mapping goes 408,000 → 2,308,000 tx/s.

Worth stating that it bought **nothing** end to end until the block store stopped being the binding
stage, and 26% afterwards (497,000 → 628,000). A faster stage behind a saturated one buys nothing.

## 6. [sidecar] Key validation and TX references allocate per transaction

Two allocation sites on the mapping path, which is what decides how fast the sidecar allocates — the
collector was 57% of its CPU under load:

- `verifyTxForm` allocates a map and the slice its keys are copied into, per namespace, to check for
  duplicate and empty keys. For the key counts that occur in practice a pairwise comparison needs
  neither.
- `TxRef` and `TxWithRef` are allocated twice per transaction, and can be backed by one slice per block.

Together 61 → 56 allocations per transaction, worth 12% at the default `GOGC`.

## 7. [sidecar] Back a block's decoded transactions with one allocation

`serialization.UnmarshalTx` declares a local `applicationpb.Tx` and returns its address, so it escapes:
one heap allocation per transaction on the mapping path.

Add a variant that unmarshals into a caller-provided transaction, and give `mapBlock` a per-block slab
for them, as it already has for `TxRef` and `TxWithRef`. 19 → 18 allocations per transaction,
identically at every block size.

The slab keeps a block's backing array alive while anything holds one element, and a
`StreamAllTransactions` subscriber can hold a transaction past the block's commit. It is bounded by the
block size and holds only message headers, so the exposure is one block's unused slots — the same trade
the existing slabs make.

## 8. [sidecar] Mapping's result carries the scaffolding that built it

`blockMappingResult` carries the three per-block slabs, a reference to the relay's in-flight TX ID set,
and the collected TX IDs. None of it is read after mapping returns — `submitSnapshotBlock` builds
further results by hand without any of them — but every mapped block in flight keeps them reachable,
and nothing stops later code reaching for a slab after the block is built.

Move that state to a builder that embeds the result it is filling, and return only the result. What it
frees is the dedup reference and the TX ID slice, one per in-flight block; it does not free the slabs,
because the result's messages live inside them.

## 9. [grpc] HTTP/2 flow control is unset and caps the pipeline

Nothing in the repository sets a gRPC window, so the defaults apply. On a saturated cluster that is the
pipeline's ceiling.

A goroutine dump of the coordinator found **all three** senders to the signature verifiers and **five of
six** to the validator-committers blocked in `grpc/internal/transport.(*writeQuota).get` — out of stream
send quota. They were not slow: each used a quarter of a core. The verifiers they feed held 1,700
transactions of a 128,000 capacity and ran 22 of 64 cores, 78% of that in real signature verification.
The senders were not allowed to write, so the verifiers starved.

### The change: a `flow-control` section, per client and per server

The right window depends on message size and round-trip time, which differ between peers, so this is a
setting rather than a constant — but the **default** has to be what sustains the measured throughput,
since a deployment that has to be tuned to reach it has not been fixed.

Add `FlowControlConfig` to `connection.ClientConfig`, `connection.MultiClientConfig` and
`serve.ServerConfig`, so every client section and every server section accepts:

```yaml
flow-control:
  initial-window-size: 16777216       # per stream
  initial-conn-window-size: 33554432  # per connection, shared by its streams
```

Semantics, per field:

| Value | Meaning |
|---|---|
| unset (0) | apply the recommended window — 16 MiB per stream, 32 MiB per connection |
| positive | apply that window |
| negative | apply no window, leaving gRPC's own BDP-based tuning in place |

Three details the implementation has to respect:

- **Both ends.** The window a peer may write into is the one *this* side advertises, so a client
  raising its own achieves nothing alone. Hence the server section as well as the client one.
- **Any explicit value disables gRPC's BDP auto-tuning**, so the values must be generous rather than
  merely adequate — a batch marshals to roughly 145 KB, and the recommended values are ~115 batches
  per stream. The negative case exists to opt tuning back in.
- **The recommended values must not be `default:` struct tags.** A tag registers a viper default for a
  key nested inside `ClientConfig`, and `ClientConfig` is reachable through optional pointer fields
  whose nil-ness is semantic — the load generator selects its adapter by which client section is
  present. Registering a default under such a pointer materialises it and silently changes adapter
  selection. Resolve the recommended value at the point of use instead.

These are credit limits, not allocations, so the cost is bounded buffering per connection and only
under overload: at a sustainable rate the deployment below held 230,000 transactions in flight, which
was its own backpressure window and essentially nothing else.

### What it is worth

Measured on the cluster, same day, same deployment shape:

| | before | after |
|---|---|---|
| over-driven mean | 510,371 tps | **578,383 tps** (+13.3%) |
| peak | 528,800 tps | **590,400 tps** |
| latency at a sustainable 500,000 tps | 645 ms | **392 ms** |

Higher throughput at lower latency. The same goroutine dump afterwards has zero senders blocked on
write quota.

## 10. Benchmarks for attributing committer performance

Three of the findings in this umbrella were invisible until the corresponding benchmark existed, and
one of them stopped a change that would have been made for nothing. A cluster tells you the pipeline
got slower; it does not tell you which stage, and a nineteen-machine baseline moves about 15% between
days, so small effects are only measurable in-process.

This issue tracks the set; each benchmark is a child, independently reviewable.

- #10.1 — end-to-end sidecar
- #10.2 — coordinator signature verifier manager
- #10.3 — load generator submit path
- #10.4 — load generator generation sweep
- #10.5 — repair the dependency graph benchmark

Shared prerequisite: several test helpers must widen from `*testing.T` to `testing.TB` so benchmarks
can reuse them (`utils/test.WaitForConnections`, `mock.StartMockVerifierService`, and the coordinator
and sidecar test environments). Precedent exists in `mock/test_exports.go`, where
`StartMockCoordinatorService` and `NewOrdererTestEnv` are already `testing.TB`.

### 10.1. [sidecar] Benchmark the whole service end to end

The sidecar has per-function benchmarks but none of the assembled service, so no way to tell which of
its stages binds without a cluster.

Run the real service on one machine against a real block store and real gRPC, with the orderer and the
coordinator stubbed, and report tx/s. Generate the transactions before the timed section, or setup
dominates the profile.

This is what found the block store ceiling (#1, #11): it put a serialized goroutine at a 100% per-block
duty cycle on a machine at 18% CPU, which no machine-level metric shows.

### 10.2. [coordinator] Benchmark the signature verifier manager

On the cluster, `coordinator_verifier_input_batch_queue_size` was the only non-empty queue in the
pipeline, which reads as the manager being unable to drain it.

Drive the real manager against mock verifiers, which return statuses without doing signature work, so
only the manager's own send/receive path is measured. Parameterise over the number of verifier
endpoints.

It measures ~560,000 tx/s on one stream and ~1.08M on three — 3.3x the cluster's per-sender rate — which
ruled the manager out before any code was changed. The queue was full because the senders were blocked
on gRPC write quota (#9), not because the manager was slow.

### 10.3. [loadgen] Benchmark the submit path

A ramp cannot separate the generator's own ceiling from the committer's: both present as the delivered
rate flattening. On this cluster the generator capped every run near 325,000 tps and the committer was
blamed for it.

Benchmark the submit path alone, per signature scheme, so the generator's ceiling is a known number
before a committer result is quoted against it.

### 10.4. [loadgen] Sweep transaction generation over core count

Generation throughput plateaus, and where it plateaus moves with the number of cores, so the right
worker count is a property of the machine that will run the generator and cannot be a fixed default.

Sweep the generation rate over worker count so the setting can be measured rather than guessed.

### 10.5. [coordinator] The dependency graph benchmark cannot run the default manager

The existing dependency graph benchmark cannot be run against the default manager, so the comparison in
#3 — 329,854 tps against the simple manager's 486,941 — had no in-process counterpart.

Fix the benchmark to cover both managers. Without it the choice between them rests on cluster runs
alone, which is the weakest evidence available for a 47.6% difference.

## 11. [blkstorage] Do not build tx index information no index will read

**Filed in `hyperledger/fabric-x-common`.** Referenced here because the committer is where the effect
is measured.

`serializeBlock` extracts a transaction ID for every envelope and builds a `txindexInfo` and a
`locPointer` for each, whatever the store is configured to index. Only two indexes read any of it: the
txID index needs the ID and the offset, and the blockNum-tranNum index needs the offset. A store
configured with neither — which is what a committer sidecar with the transaction ID index disabled
does, since it indexes by block number alone — pays for all of it and reads none.

The transaction ID is the expensive half: `GetOrComputeTxIDFromEnvelope` unmarshals the whole envelope
and its payload header per transaction. On a nineteen-machine cluster driving 10,000-transaction blocks,
a CPU profile of the sidecar put `addDataBytesAndConstructTxIndexInfo` at 7.4% of the whole process and
about 88% of `appendBlock`. Because the append is one serialized goroutine, that was the pipeline's
ceiling: the stage ran at a 100% duty cycle — 22.2 ms of a 22.2 ms per-block budget — on a machine
sitting at 18% CPU with its disk 4% busy.

Have `serializeBlock` ask the index what it will read. Append 22.2 → 7.1 ms per block, which lifted the
sidecar's own ceiling from ~451,000 tps. The serialized bytes are unchanged for every combination.
