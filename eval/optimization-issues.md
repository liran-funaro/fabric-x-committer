<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->

# Optimization issues

The issues opened for the work in `optimization-summary.md`. Everything with a number is filed and
the numbers are the real ones, with GitHub sub-issue links mirroring the parent/child structure.
Three entries are drafted but **not yet opened**, and are marked as such in the table. Two entries have a
pull request and no issue behind them at all — #181, which is merged, and #815, which is open — because the
work was raised straight as a pull request; both are recorded here so the set is complete.

**Status column, checked against GitHub on 2026-09-08.** `filed` means open with no pull request yet;
`PR open` names the pull request; `resolved` means the change is merged. Three are resolved: the two
fabric-x-common changes the paper-figure measurements were taken against (#166 and #181), and #789 in the
committer. Note that a merged change cannot be found by looking for its commit in `main` — these branches
are squash-merged, so the SHAs in `optimization-summary.md` are the pre-merge ones and exist only on the
evaluation branch. The pull request state is what to trust.

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
| #798 | Committer throughput and latency: findings from the nineteen-machine evaluation | committer | filed |
| #784 | [sidecar] Make the block store transaction ID index optional | committer | filed |
| #772 | [sidecar] The relay tracks in-flight blocks and TX IDs in sync maps on its per-TX path | committer | **PR open** — #814 |
| #791 | [coordinator] Allow selecting the simple dependency graph manager | committer | filed |
| #785 | [coordinator] The simple dependency graph can latch the pipeline under load | committer | filed |
| #786 | [sidecar] Parse a block's transactions in parallel | committer | filed |
| #787 | [sidecar] Key validation and TX references allocate per transaction | committer | filed |
| #788 | [sidecar] Back a block's decoded transactions with one allocation | committer | filed |
| #789 | [sidecar] Mapping's result carries the scaffolding that built it | committer | **resolved** — PR #800 merged |
| #790 | [grpc] Add a per-client and per-server `flow-control` section; HTTP/2 windows are unset and cap the pipeline | committer | filed |
| #797 | Benchmarks for attributing committer performance | committer | filed |
| #792 | [sidecar] Benchmark the whole service end to end | committer | filed |
| #793 | [coordinator] Benchmark the signature verifier manager | committer | filed |
| #794 | [loadgen] Benchmark the submit path | committer | filed |
| #795 | [loadgen] Sweep transaction generation over core count | committer | filed |
| #796 | [coordinator] Sweep the dependency graph benchmark over the constructor pool | committer | filed |
| hyperledger/fabric-x-common#165 | [blkstorage] Do not build tx index information no index will read | **fabric-x-common** | **resolved** — PR #166 merged |
| — | [utils] The load generator builds an HMAC-DRBG for every ECDSA signature | committer | **needs opening** |
| hyperledger/fabric-x-common#181 | [applicationpb] The signing digest is built by reflection | **fabric-x-common** | **resolved** — PR #181 merged, no issue was opened |
| #815 | [coordinator] Hold a waiting key's first group inline | committer | **PR open** — #815, opened without an issue |
| — | [loadgen] Block preparation caps the generator at small block sizes | committer | **needs opening** |
| — | [testcrypto] Preparing a block clones and rehashes it unconditionally | **fabric-x-common** | **needs opening** |

---

## #798. Committer throughput and latency: findings from the nineteen-machine evaluation

An evaluation on nineteen machines — one sidecar, one coordinator, three signature verifiers, six
validator-committers and a twelve-node YugabyteDB — took the committer from 80,000 to 500,000
transactions per second sustained, and identified where the remaining constraint is.

This issue collects the changes that produced it. Each child is independently reviewable; the
measurement that motivated each one is in its own issue, and the full account with all the evidence,
including the retracted findings, is in `docs/cluster-optimization-log.md` and
`docs/optimization-summary.md`.

Children: #784 #772 #791 #785 #786 #787 #788 #789 #790 #797 (itself the parent of the five benchmark
issues).

One change lives outside this repository, so it cannot be linked as a sub-issue:
**hyperledger/fabric-x-common#165** — *[blkstorage] Do not build tx index information no index will
read*. `serializeBlock` extracts a transaction ID per envelope and builds a `txindexInfo` and a
`locPointer` for each, whatever the store is configured to index, and a sidecar that indexes by block
number alone reads none of it. Because the append is one serialized goroutine, that was the pipeline's
ceiling: 22.2 ms of a 22.2 ms per-block budget on a machine at 18% CPU. Append went 22.2 ms → 7.1 ms,
lifting the sidecar's own ceiling from ~451,000 tps.

It pays off only together with #784 — a store still indexing transaction IDs genuinely needs that
information — so the two belong in the same deployment.

Where the constraint ends up: not in the committer. At 500,000 tps every committer stage has headroom
— sidecar 14% CPU, coordinator 21%, verifiers 34%, validator-committer preparer 4 of 96 workers busy —
while the database runs 83% CPU and 88% disk busy. Raising throughput further means writing fewer
bytes per transaction, not tuning the committer.

## #784. [sidecar] Make the block store transaction ID index optional

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

## #772. [sidecar] The relay tracks in-flight blocks and TX IDs in sync maps on its per-TX path

Already open as **#772**, and **PR #814 is open against it** (`sidecar-relay-single-owner-tracking`).
No new issue.

## #791. [coordinator] Allow selecting the simple dependency graph manager

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
unfixed defect (#785).

## #785. [coordinator] The simple dependency graph can latch the pipeline under load

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

Prerequisite for #791 rather than a gain in itself: the manager cannot be recommended until these are
fixed. A regression test per defect belongs with the fix.

## #786. [sidecar] Parse a block's transactions in parallel

`mapBlock` parses and validates a block's messages one at a time on a single goroutine, and mapping is
the sidecar's largest per-block stage.

Parse across up to 16 goroutines, then fold the results into the block in message order so that the
transaction-ID dedup set and the batch order stay single-threaded and the outcome does not depend on
how the parsing was split. Mapping goes 408,000 → 2,308,000 tx/s.

Worth stating that it bought **nothing** end to end until the block store stopped being the binding
stage, and 26% afterwards (497,000 → 628,000). A faster stage behind a saturated one buys nothing.

## #787. [sidecar] Key validation and TX references allocate per transaction

Two allocation sites on the mapping path, which is what decides how fast the sidecar allocates — the
collector was 57% of its CPU under load:

- `verifyTxForm` allocates a map and the slice its keys are copied into, per namespace, to check for
  duplicate and empty keys. For the key counts that occur in practice a pairwise comparison needs
  neither.
- `TxRef` and `TxWithRef` are allocated twice per transaction, and can be backed by one slice per block.

Together 61 → 56 allocations per transaction, worth 12% at the default `GOGC`.

## #788. [sidecar] Back a block's decoded transactions with one allocation

`serialization.UnmarshalTx` declares a local `applicationpb.Tx` and returns its address, so it escapes:
one heap allocation per transaction on the mapping path.

Add a variant that unmarshals into a caller-provided transaction, and give `mapBlock` a per-block slab
for them, as it already has for `TxRef` and `TxWithRef`. 19 → 18 allocations per transaction,
identically at every block size.

The slab keeps a block's backing array alive while anything holds one element, and a
`StreamAllTransactions` subscriber can hold a transaction past the block's commit. It is bounded by the
block size and holds only message headers, so the exposure is one block's unused slots — the same trade
the existing slabs make.

## #789. [sidecar] Mapping's result carries the scaffolding that built it

**Resolved.** PR **#800** is merged and the issue is closed. The description below is what was filed.

`blockMappingResult` carries the three per-block slabs, a reference to the relay's in-flight TX ID set,
and the collected TX IDs. None of it is read after mapping returns — `submitSnapshotBlock` builds
further results by hand without any of them — but every mapped block in flight keeps them reachable,
and nothing stops later code reaching for a slab after the block is built.

Move that state to a builder that embeds the result it is filling, and return only the result. What it
frees is the dedup reference and the TX ID slice, one per in-flight block; it does not free the slabs,
because the result's messages live inside them.

## #790. [grpc] HTTP/2 flow control is unset and caps the pipeline

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

## Needs opening. [utils] The load generator builds an HMAC-DRBG for every ECDSA signature

To be filed against the committer. The change is already implemented (`b1dc7dc6`) and is waiting on
this issue to reference; the evidence is in `cluster-optimization-log.md` and the benchmark numbers in
section 1 of the summary.

`crypto/ecdsa.SignASN1` signs "hedged" per FIPS 186-5: every signature reads 32 bytes of entropy and
then builds a fresh HMAC-SHA-512 DRBG personalized with the private key and the digest. On this
cluster that cost **1.34x the `k*G` scalar multiplication it exists to feed** — `newDRBG` 8.11% of
process CPU and `hmacDRBG.Generate` 3.69% against `ScalarBaseMult`'s 8.80% — and `newDRBG` alone was
23% of everything the generator allocated. The entropy read itself is 0.54%.

`utils/testsig` now derives the nonce itself, reaches the same assembly-optimized P-256 through
`elliptic.P256().ScalarBaseMult`, and emits the signature's DER with `cryptobyte` instead of
reflection-driven `encoding/asn1`. Worth 2.14x on the signing path at the deployment's settings
(2,920 -> 1,364 ns/op on 32 cores at GOGC=400) and 6,064 -> 2,249 B/op.

Two things the issue has to say beyond the change itself:

- It is safe **here specifically**. Hedging protects a private key against an RNG failure; this
  package signs synthetic transactions with throwaway test identities, and the signatures remain
  ordinary ECDSA that `ecdsa.VerifyASN1` accepts unchanged.
- It retires two explanations recorded earlier in this evaluation. "The getrandom syscall" is 0.54%
  of CPU, and that claim was load-bearing for `loadgen_workers: 128` — which therefore now has no
  justification behind it and wants re-sweeping at 64. "Allocation" was right but non-specific: the
  allocation has one dominant source, and with `GOGC=400` the collector itself is only ~4.9%.

`gnark-crypto`'s `secp256r1/ecdsa` was measured first and rejected at 306,730 ns/op, **8x slower**
than the standard library, because its generic big.Int field arithmetic swamps any nonce saving.
Worth recording so it is not tried again.

## #815. [coordinator] Hold a waiting key's first group inline

**PR #815 is open**, and there is no issue behind it — it was raised straight as a pull request, which is
why nothing in these documents referenced it until now. Recorded here for completeness; the account is in
`cluster-optimization-log.md` §4.5 and the figures in the summary at 1.10 and 3.6.

`SimpleManager.checkTXFree` built four heap objects for every key of every transaction that found its key
free: the `waiting`, its `queue` slice, a `waiterGroup`, and that group's `[]*TransactionNode`. All four
share a lifetime and become garbage together when the key is released, so an uncontended workload — a key
claimed once and released once — paid four allocations to describe a queue of one. Holding the first group
and its first member inline as fields of `waiting` makes that case one allocation, and leaves `add`
untouched, so a second group still moves the queue to the heap and contended ordering is unchanged.

Allocations per transaction fall by three per key: 13 → 10 at one, 30 → 18 at four, 53 → 29 at eight.

Three things any reader of the PR needs, and they are the reason it should not be read as a throughput
change:

- **It saves no measurable time**, and that is the result rather than a caveat. Six runs per arm gave
  +3.3% and +4.2%, inside a spread reaching 54%, and a second machine came out 3% the other way. An
  earlier three-run comparison suggested 13% and was wrong for that reason.
- **`SimpleManager` has no production caller.** The coordinator always constructs the global-local
  manager, so this is inert on `main` and becomes live only if #791's selection wiring lands. The
  benchmark it adds is not inert: it covers both managers.
- The reason to want it is **memory**, not speed — this deployment's coordinator reached 79 GB of RSS at
  about 4 KB retained per transaction.

The durable finding from the same work is separate from the allocation change and outlives it: the graph's
cost is **per key**, about 420 ns each and flat from one key per transaction to eight, so a
four-read-write transaction costs four times a one-read-write transaction. That is what produces 9a's fall
in `paper-figures.md`. It also contradicts the paper's own explanation, which is lock contention in the
dependency graph: on this configuration both lock-wait histograms have a count rate of exactly zero,
because the simple manager takes those loops out of the path.

## Needs opening. [loadgen] Block preparation caps the generator at small block sizes

To be filed against the committer. The change is implemented on `eval/fast-block-prepare` (`ae27afe4`)
and pairs with the fabric-x-common issue below it, which is what makes the cheap path possible. The
evidence is in `cluster-optimization-log.md` §5.

The sidecar adapter cuts its own blocks and hands each to an embedded mock orderer, whose single
goroutine calls `testcrypto.PrepareBlockHeaderAndMetadata` before serving it. That deep-clones the block
and hashes all of its data, and both scale with the block: 0.16 ms and 0.54 ms for 500 transactions of
300 bytes, 3.1 ms and 11.1 ms for 10,000, against 0.05 ms for everything else the call does. Fitting the
cluster's two block sizes gives 1.62 µs per transaction against the benchmark's 1.68 µs for the clone plus
the hash, so that pair is the whole per-transaction cost of preparing a block.

The consequence is a measurement problem, not a product one. The evaluation's 500-transaction ladder
stopped at 853 blocks a second, which is the generator, so the committer's ceiling at that block size was
never measured and the block-size trade-off is quoted from a generator-bound point.

`fast-block-prepare` moves `ComputeBlockDataHash` into the adapter's mapper goroutine — one stage ahead,
and nearly idle, because transactions arrive already serialized — and lets the orderer prepare in place.
Preparation falls to 8.4 µs at 500 transactions and 8.3 µs at 10,000, independent of block size.

Three things the issue should say beyond the change:

- **Off by default**, so a figure taken with it is never silently compared against one taken without it.
  What the committer receives is identical either way; only the cost of producing it changes.
- The expected cluster gain is **about 2×, not the 72× the stage benchmark shows**. Preparation was
  roughly half the generator's per-block budget, and the mapper now carries the hash, which becomes the
  next limit near 1,850 blocks a second. Beyond that the hash wants an ordered pool, not one goroutine.
- It retires nothing already recorded, but it does add a second generator limit alongside signing. §5 of
  the log previously named signing as the reason the committer's ceiling is unknown above ~370,000 tps;
  at small block sizes block preparation binds first.

One finding for a separate issue, not fixed here: the mock orderer's *envelope* path — used when a client
broadcasts rather than submitting whole blocks — SHA-256s and base64-encodes every payload for a dedup
cache, 1.17 µs per transaction, and then the block data hash SHA-256s the same bytes again. The cache
cannot be turned off, because `payload-cache-size: 0` means "use the default" of 1024, and 1024 entries at
400,000 tps is a 2.4 ms replay window. This adapter never uses that path, so it is not what capped
anything measured here.

## Needs opening. [testcrypto] Preparing a block clones and rehashes it unconditionally

To be filed against fabric-x-common. **An implementation already exists** on `eval/fast-block-prepare`
(`fc7b1c8a`). It is the enabling half of the committer issue above.

`PrepareBlockHeaderAndMetadata` opens with `proto.CloneOf(block)` and then sets
`DataHash: ComputeBlockDataHash(block.Data)`. Those two are the entire cost of the call — see the table in
the issue above — and neither is always needed:

- The clone makes it safe to prepare a block the caller intends to reuse or submit twice. A producer that
  builds a block for this call alone pays a full copy of every transaction for nothing.
- The data hash covers the block's own data. Unlike the number and the previous hash it does not depend on
  the chain, so it does not have to be computed in chain order and a producer can compute it off the
  critical path.

`InPlace` and `ReuseDataHash` make each opt-in. `ReuseDataHash` is ignored when the block carries no
header or an empty hash, so a caller that sets it and forgets to supply one gets a correct block rather
than a silently corrupt one.

The test is an equivalence test, which is the only thing that makes the options worth having: a block
prepared the fast way is proto-identical to one prepared the cloning way, in place returns the object it
was given, and reusing a hash that was never supplied still produces the right one.

Worth stating in the issue that misuse is loud rather than silent. A submitter that reuses one block while
preparing in place hands a block cache several entries that alias one object, and a consumer waiting for a
number that has been overwritten stalls; it does not read a wrong block. That was found by writing a
benchmark that did exactly this.

## hyperledger/fabric-x-common#181. [applicationpb] The signing digest is built by reflection

**Resolved.** No issue was ever opened: the work went straight to a pull request, and PR
**hyperledger/fabric-x-common#181** is merged. The change is in that repository's `main` as `3818db81a`,
and it is the second of the two upstream changes the paper-figure measurements were taken against.

Upstream's merged version inlines the two length helpers this branch kept separate; the encoding is
identical either way, which was verified by diffing the branch against `main` and running the branch's
own tests against it. The review notes below are from measuring the implementation, and the artefacts are
in `fx-cluster-logs/asn1-review/`.

`TxNamespace.ASN1Marshal` builds the digest that every transaction is signed over and that every
verifier recomputes. It translates the namespace into an intermediate struct tree and hands that to
`encoding/asn1.Marshal`, which is reflection driven. A CPU profile of the load generator at 451,826
tps put `encoding/asn1.Marshal` at 14.31% of process CPU, of which **80.5% is this digest** (11.5% of
the process) and 19.5% was the signature's own DER; `asn1.makeField` is 85.9% of the marshal. For
comparison the elliptic curve multiplication in the same signature is 21.79%, so building the message
to sign cost over half of signing it. The signature half is already fixed (`b1dc7dc6`, `cryptobyte`).

The branch adds `QuickASN1Marshal` alongside the existing method rather than replacing it, and wires a
byte-equality assertion into `requireASN1Marshal`, so `FuzzASN1MarshalTxNamespace` compares the two on
every input. That is the right shape: the output must stay byte-identical or every signature in every
deployment becomes invalid.

Three findings from reviewing it:

- **Byte-identical under fuzzing.** 2.96 million executions with no mismatch.
- **One divergence the fuzzer cannot reach.** Its harness always passes a two-element metadata slice,
  so nil and empty are never compared. For a non-nil *empty* `metadata`, `encoding/asn1` emits an
  empty SEQUENCE — its `optional` rule omits only on `DeepEqual` to the zero value, which is nil —
  while `QuickASN1Marshal`'s `len(metadata) > 0` omits it, giving a different digest. Changing that
  guard to `metadata != nil` fixes it, and a further 1.35 million fuzz executions pass. Not reachable
  through the load generator (which builds nil or one element) or protobuf decoding (empty repeated
  fields decode to nil), so latent rather than active.
- **Verified on the cluster.** Both call sites switched (`utils/signature`'s verifier and
  `utils/testsig`'s signer), drained 300 s holds: 460,000 tps at 380 ms / 580 ms p99 against 390 /
  600 before, and 480,057 tps at 460 ms / 700 ms p99, with zero aborts throughout. The ceiling did
  not move — instead the generator stopped being the sole constraint, and at ~496,400 tps the load
  generator (78.4%) and the database machines (73.8-79.4%) sit at the same utilisation. See
  `cluster-optimization-log.md` 4A.6.
- **It is 4-5x faster for realistic shapes and slower for very large values.** At the shape this
  cluster generates, two read-writes with 32-byte keys and values: 7,510 -> **1,512 ns/op** and 61 ->
  **9 allocations**. The ratio holds at 8, 64 and 512 read-writes. But on the `varying length` test
  fixture, whose keys run to 1 MB, it is **4.7x slower** and allocates 5.5 MB against 1.25 MB, because
  nested `bytes.Buffer`s copy the payload three times — element into child sequence, child into main
  sequence, main into the outer wrap — where `encoding/asn1` sizes its output once. Worth resolving
  before submission if large values are a supported workload.

## #797. Benchmarks for attributing committer performance

Three of the findings in this umbrella were invisible until the corresponding benchmark existed, and
one of them stopped a change that would have been made for nothing. A cluster tells you the pipeline
got slower; it does not tell you which stage, and a nineteen-machine baseline moves about 15% between
days, so small effects are only measurable in-process.

This issue tracks the set; each benchmark is a child, independently reviewable.

- #792 — end-to-end sidecar
- #793 — coordinator signature verifier manager
- #794 — load generator submit path
- #795 — load generator generation sweep
- #796 — repair the dependency graph benchmark

Shared prerequisite: several test helpers must widen from `*testing.T` to `testing.TB` so benchmarks
can reuse them (`utils/test.WaitForConnections`, `mock.StartMockVerifierService`, and the coordinator
and sidecar test environments). Precedent exists in `mock/test_exports.go`, where
`StartMockCoordinatorService` and `NewOrdererTestEnv` are already `testing.TB`.

### #792. [sidecar] Benchmark the whole service end to end

The sidecar has per-function benchmarks but none of the assembled service, so no way to tell which of
its stages binds without a cluster.

Run the real service on one machine against a real block store and real gRPC, with the orderer and the
coordinator stubbed, and report tx/s. Generate the transactions before the timed section, or setup
dominates the profile.

This is what found the block store ceiling (#784, hyperledger/fabric-x-common#165): it put a serialized goroutine at a 100% per-block
duty cycle on a machine at 18% CPU, which no machine-level metric shows.

### #793. [coordinator] Benchmark the signature verifier manager

On the cluster, `coordinator_verifier_input_batch_queue_size` was the only non-empty queue in the
pipeline, which reads as the manager being unable to drain it.

Drive the real manager against mock verifiers, which return statuses without doing signature work, so
only the manager's own send/receive path is measured. Parameterise over the number of verifier
endpoints.

It measures ~560,000 tx/s on one stream and ~1.08M on three — 3.3x the cluster's per-sender rate — which
ruled the manager out before any code was changed. The queue was full because the senders were blocked
on gRPC write quota (#790), not because the manager was slow.

### #794. [loadgen] Benchmark the submit path

A ramp cannot separate the generator's own ceiling from the committer's: both present as the delivered
rate flattening. On this cluster the generator capped every run near 325,000 tps and the committer was
blamed for it.

Benchmark the submit path alone, per signature scheme, so the generator's ceiling is a known number
before a committer result is quoted against it.

### #795. [loadgen] Sweep transaction generation over core count

Generation throughput plateaus, and where it plateaus moves with the number of cores, so the right
worker count is a property of the machine that will run the generator and cannot be a fixed default.

Sweep the generation rate over worker count so the setting can be measured rather than guessed.

### #796. [coordinator] Sweep the dependency graph benchmark over the constructor pool

`BenchmarkDependencyGraph` covers both managers but exercises the default one at only 2 and 4 local
dependency constructors, so it cannot say whether that pool is what bounds it — the question anyone
tuning `num-of-local-dep-constructors` is actually asking.

Widen the sweep to 1 / 2 / 4 / 8 / 16 / 32. The answer is that the pool is not the bound: 216,886 /
249,691 / 220,000 / 246,929 / 221,484 / 228,068 tx/s, no trend, because the ceiling is in the global
manager's two single goroutines. That is worth pinning in a benchmark, since the setting reads like a
throughput knob and is not one.

Number the batches from 1 while there, as the coordinator's own numbering does. Batch 0 does
`CompareAndSwap(0-1, 0)` against a counter starting at 0, which never succeeds, so it parks one
constructor goroutine permanently and loses that batch. With two or more constructors the rest carry
the load and the reported rate is unaffected — 247,439 against 249,222 tx/s — but the 1-constructor
case hangs outright, so the sweep cannot be widened without it.

## hyperledger/fabric-x-common#165. [blkstorage] Do not build tx index information no index will read

**Resolved.** PR **hyperledger/fabric-x-common#166** is merged and the issue is closed; the change is in
that repository's `main` as `ce8ca3d6d`. It is one of the two upstream changes the paper-figure
measurements were taken against — see `paper-figures.md` on which fabric-x-common the cluster ran.

**Filed as `hyperledger/fabric-x-common#165`**, and named in the umbrella's body since a sub-issue
cannot cross repositories. Recorded here because the committer is where the effect is measured, and it
pairs with #784, which is what configures a store to index by block number alone.

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
