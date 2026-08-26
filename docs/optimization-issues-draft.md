<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->

# Optimization issues: draft

Drafts for the issues to open for the work in `optimization-summary.md`. Nothing here is filed yet.

One umbrella issue plus a child per change. Evidence for every number is in
`cluster-optimization-log.md`; the issue bodies below state only the change and what drove it.

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
| 9 | [grpc] HTTP/2 flow control is unset and caps the pipeline | committer | new |
| 10 | Benchmarks for attributing committer performance | committer | new |
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

Children: #1 #2 (#772) #3 #4 #5 #6 #7 #8 #9 #10, plus one in `fabric-x-common` for the block store's
unused transaction index information.

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

Add a setting to select the simple manager. It is not a replacement: the default manager tracks
dependencies the simple one does not, so this is a deployment choice, not a default change.

## 4. [coordinator] The simple dependency graph can latch the pipeline under load

Under sustained load the simple manager can stop releasing transactions and never resume. Everything
downstream continues to look healthy — queues drain, no errors are logged — so it presents as a hang
rather than a failure.

Prerequisite for #3 rather than a gain in itself: the manager cannot be recommended until this is
fixed. A regression test that reproduces it belongs with the fix.

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

Set `InitialWindowSize` and `InitialConnWindowSize` on both ends — the window a peer may write into is
the one this side advertises. Sized against what is on the wire: a batch marshals to roughly 145 KB.
Setting either option disables gRPC's own BDP-based tuning, so the values must be generous rather than
merely adequate. They are credit limits, not allocations: at a sustainable rate the whole pipeline holds
230,000 transactions, which is the sidecar's own window and essentially nothing else.

Measured on the cluster, same day, same deployment shape: over-driven mean 510,371 → 578,383, peak
528,800 → 590,400, and latency at a sustainable 500,000 tps **645 ms → 392 ms**. Higher throughput at
lower latency. The same dump afterwards has zero senders blocked on write quota.

## 10. Benchmarks for attributing committer performance

Three of the findings in this umbrella were invisible until the corresponding benchmark existed, and
one of them stopped a change that would have been made for nothing.

- **An end-to-end sidecar benchmark** — the whole service on one machine with a real ledger and real
  gRPC, orderer and coordinator stubbed. The only way to attribute a sidecar stage without a cluster;
  it found the block store ceiling.
- **A signature-verifier-manager benchmark** — the real manager against mock verifiers that do no
  signature work. It measures ~560,000 tx/s on one stream and ~1.08M on three, 3.3× the cluster's
  per-sender rate, which ruled the manager out as the constraint before any code was changed.
- **A load generator submit-path benchmark** — separates the generator's ceiling from the committer's,
  which a ramp cannot do.
- **Generation sweeps** — the generator's plateau moves with core count, so a setting has to be
  measured on the machine that will run it.
- **A fix to a coordinator benchmark that could never run the default manager**, which made the manager
  comparison in #3 valid.

Several shared test helpers need widening from `*testing.T` to `testing.TB` so benchmarks can reuse
them.

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
