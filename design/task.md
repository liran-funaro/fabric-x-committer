# Task 3 — State Divergence Localization and Repair (snapshot/checkpoint EPIC, phase 2)

Owner per the phase-2 split with Senthil: Liran takes State Divergence Localization and Repair
(SDLR), the divergence check and fix.
Senthil takes pruning (block store) and bootstrapping / backup-of-clone-to-file.

## Context already in the tree

- Divergence is *detected* today, in two steps. The snapshot hasher
  (`service/snapshothasher/`, one instance alongside any number of VCs, discovering work
  from the durable `_snapshot` record rather than from a notification) hashes the clone and
  stores the result on that record. The VC then verifies each `_checkpoint` TX against it
  before the TX commits (`rejectCheckpointIfNotVerified`, `service/vc/checkpoint.go:45`).
  A mismatch drops the checkpoint write, records both hashes in `SnapshotState.error`
  (`haltOnCheckpointDivergence`, `service/vc/checkpoint.go:117`), and returns
  `CheckpointFeedback.HALT` (`api/servicepb/common.proto:48`) with the same text in
  `reason`. The sidecar then stops block intake with a non-retryable error
  (`processCheckpointFeedback`, `service/sidecar/relay.go:446`), and the clone is a
  frozen, immutable copy of the diverging state.
- The state DB is ahead of the clone when the committer halts. The sidecar drains only
  around the snapshot TX itself (`submitSnapshotBlock`, `service/sidecar/relay.go:225`), so
  intake keeps running until the checkpoint TX arrives, and the state DB holds every block
  between the two.
- A clone holds the state just before its snapshot TX. It is created before the snapshot
  TX's batch commits (`service/vc/committer.go:133`), so it contains the regular
  transactions of the snapshot's own block but neither the snapshot TX's `tx_status` row
  nor its `_snapshot` record.
- A hash still computing is not a divergence. A checkpoint that arrives first gets
  `CheckpointFeedback.HOLD`, and the sidecar re-submits it every
  `checkpoint-hold-retry-interval` (default 1m). A checkpoint naming no snapshot, or a
  snapshot other than the latest, or one already `CHECKPOINTED` or `ABORTED`, is rejected
  with `REJECTED_CHECKPOINT_NO_SUCH_SNAPSHOT` and never halts: `_checkpoint` is
  client-submittable.
- A snapshot can be closed without comparing its hash. A `_checkpoint` write whose value is
  the exact string `abort` (`servicepb.CheckpointAbortValue`) moves the snapshot to `ABORTED`.
  An abort is never a verified checkpoint, so rollback never restores an `ABORTED` clone.
- Only the **root** hash is persisted (`committerpb.SnapshotState.hash`). Per-table
  digests are computed and discarded — see the explicit phase-2 `NOTE` at
  `service/snapshothasher/hasher.go:87`.
- Hashed set (`listHashedTables`): every user namespace `ns_<id>` from `ns__meta`, plus
  `ns__config`, `ns__meta`, `tx_status`, `ns__checkpoint`. `metadata` and `ns__snapshot`
  are excluded. Rows are folded in primary-key order as `len(k)||k||len(v)||v`, plus an
  8-byte big-endian version for namespace rows; table digests are combined in a fixed order
  (fixed system tables, then `ns__meta` registry order).
- A checkpoint is keyed by the snapshot's block number alone (`servicepb.CheckpointKey`,
  `api/servicepb/checkpoint_key.go`), and the clone is named after it
  (`snapshot_<block>`). Both the comparison point and the clone are therefore identified
  by one block number.

So detection exists. What is missing is **localization** and **repair**.

## R1 — Localization data (extend, do not replace)

1. The hash job persists three levels of digest for every hashed table: the root hash as
   today, one digest per table, and one digest per key-prefix bucket. Two orgs then rule
   out whole namespaces table-by-table, narrow to a bucket, and exchange rows only for
   that bucket.
2. A bucket is a contiguous key range — the rows whose key shares its first K bits, so a
   table holds at most 2^K buckets. The bucket level is flat: a bucket digest is compared
   directly and never descended into.
   - `tx_status` takes the first 2K bits instead. Its keys are TX IDs, which are hex text:
     each byte is one of 16 characters, so K bits carry only K/2 bits of entropy. With K=16,
     two million TX IDs fill 256 of the 65,536 buckets at K bits, and all 65,536 at 2K bits
     (at most 57 rows each, against a mean of 30.5).
   - The bucket number never decreases in primary-key order. That is what lets one hasher
     stay live and makes each bucket a contiguous range. A raw-byte prefix has this property
     by construction; any other bucket function must keep it.
   - Hex-decoding the TX ID before taking the prefix is ruled out by the same property. The
     committer accepts any non-empty TX ID, so decoded and raw prefixes would mix and break
     key order. With 2K raw bits, a non-hex ID only lands in a sparser bucket.
   - Bucket numbers are `uint32`, so 2K ≤ 32 and K ≤ 16.
3. Each row is folded exactly once, into its bucket. The per-table digest is derived from
   its bucket digests and the root from the per-table digests, which is why the
   localization data measures within noise of today's single fold.
4. K is carried by the snapshot TX as its single read-write, exactly the shape the
   `_checkpoint` namespace already requires (`service/sidecar/mapping.go:590`): one
   read-write with a nil version, no reads-only, no blind writes. Its key is `bucket-bits`
   and its value one byte holding K. The `_snapshot` namespace is marker-only today
   (`Status_MALFORMED_SNAPSHOT_NOT_MARKER_ONLY`, `service/sidecar/mapping.go:581`), and its
   form check changes to require that read-write.
   - The read-write is never applied. The preparer replaces the `_snapshot` namespace with
     the record it synthesizes and skips the TX's own writes (`service/vc/preparer.go:194`,
     `:205`), so no snapshot overwrites the previous one's K. The preparer copies K into a
     new `SnapshotState` field.
   - The clone step rebuilds the record from scratch (`service/vc/database_snapshot.go:235`)
     and must copy K forward. Every later update decodes and edits the existing record, so
     K survives those.
   - The coordinator puts the read-write's key into its dependency graph
     (`service/coordinator/dependencygraph/transaction_node.go:187`), so every snapshot TX
     depends on the one before. That costs nothing: the sidecar drains before and after
     each snapshot TX, so two are never in flight together.
   - K is endorsed: the namespace signature covers its read-writes, so
     `SnapshotEndorsement` covers K.
   - K's permitted range is a constant in code, at most 16 (see 2), and a submitter may
     size K within that range from its own row-count estimate.
5. The digests live in the source state DB, not in a file. The hasher writes them in the
   same transaction that moves the record to COMPLETED, so a committed root always has its
   digests. `committer db divergence export` reads them and writes the digest file that is
   sent to a peer. The file is a transfer format, never the stored copy.
6. Every digest set carries K and a format version, in the DB and in the exported file's
   header, so either is interpretable on its own. The clone is created before its own
   `_snapshot` record commits (`service/vc/database_snapshot.go:229`), so K is reachable
   nowhere else.
7. The persisted digests are excluded from the hashed set and are deleted with their clone.
   Hashing them would fold each snapshot's digest into the next; tying them to the clone
   puts them under `committer db clone delete` (R5) instead of a second retention policy.
8. Digests are deterministic and content-only: identical content ⇒ identical digests,
   regardless of table-completion order, page size, worker count, or node count.
9. Digests are recomputable offline by a tool with no running committer, from a clone, a
   backup file, or the state DB itself — the committer is halted exactly when this is
   needed, and with no writer there is no cut to take.
10. Computing the digests must not disturb a live cluster: reuse the existing bounded keyset
    pagination and worker limits, adding no concurrent read load beyond the configured
    limits.

## R2 — Compare tool (`ledgerutil compare` analogue)

- Every SDLR command is under `committer db divergence`, next to the existing `db init`
  and `db clone` commands: `export`, `compare`, `export-rows`, `report` and
  `repair {patch,rollback,reset}`. Each one reads the state DB, like the rest of
  `committer db`.
- Compare takes one input: the peer's exported digest file. The local digests come from
  the state DB, never from a file, so an operator cannot compare against a stale or wrong
  local export. Compare refuses a peer file whose snapshot block, K or format version
  differs from the local record.
- Inputs that are not digests — the local clone, or a backup file — are snapshot sources
  for recomputing digests (R1.9). Backup-to-file comes from the bootstrapping task; treat
  it as a dependency, not a deliverable here.
- Exchange digests / subtree hashes, not full state: bandwidth proportional to the
  divergence, not to the database.
- Output both classes, both are needed:
  - **`tx_status` diffs** — txID, status, height. Primary signal: a mishandled TX.
  - **key/value diffs** — namespace, key, value, version. Needed for data corruption.
- Report which tables agree, so whole namespaces can be ruled out immediately.

## R3 — Troubleshoot output

- For each divergent key, name the TX(s) that wrote it and their block / TX position, so
  the manual root-cause hunt starts from a block number and not a hex key.
- Human-readable, file-based report — no service, no API. Operators run this once, under
  pressure.

## R4 — Repair tools

Every repair restores the state DB from a clone and re-consumes the ledger from that clone's
cut. The modes differ in which clone, and in what happens before the replay.

- **Rollback**: restore from the previous validated clone, then re-consume every block since
  its cut. Needs no diff and no peer, at the cost of re-consuming a full snapshot interval of
  load. The replay runs the same committer code that diverged, so it is not trusted to be
  correct: it is verified against the checkpoint like every other mode.
- **Patch**: restore from the latest (diverging) clone, replace the buckets R2 identified
  with the peer's rows, verify, and only then re-consume the blocks committed after that
  clone's cut. The replay window runs from the snapshot to its checkpoint, not over a full
  snapshot interval.
- **Reset**: drop the state DB and rebuild by replaying the ledger from genesis (or from
  the bootstrap snapshot).
- The repair unit is the bucket, not the row. A divergent bucket's whole key range is
  replaced by the peer's rows for that range, which handles missing and extra rows without
  per-row diff semantics and makes the set of buckets to re-validate exactly the set
  repaired.
- A patch verifies before it replays. Recompute the digests of the repaired buckets, re-fold
  them with R1's persisted digests for every untouched bucket into the table digest and the
  root, and confirm the root now matches the checkpoint hash — all at the clone's cut, which
  is the only height the checkpoint attests to. The checkpoint was endorsed by a majority, so
  this proves the repair is correct, not merely that it agrees with one peer. A peer that
  sent wrong rows produces a patch that fails here, before anything is replayed.
- The halt must record the checkpoint hash as a field, because the patch verifies against it
  offline. Today `haltOnCheckpointDivergence` (`service/vc/checkpoint.go:117`) writes both
  hashes only into `SnapshotState.error`. fabric-x-common already defines
  `SnapshotState.checkpoint_hash` for this, and nothing in the committer sets it yet.
- Every repair is then verified again by the existing checkpoint path. The halted checkpoint
  has no `tx_status` row, so the VC re-verifies it when replay re-delivers it, against the
  hash of the snapshot it just recreated. A wrong repair halts again; a right one commits.
- Rollback and reset are verified at that replayed checkpoint, by full rehash. Both change
  every table below the restored height, so no persisted bucket digest survives them, but
  they do not have to wait for a later snapshot.
- Re-validation reads the repaired state DB directly, taking no fresh clone. A clone exists
  to give a consistent cut of a live cluster; the committer is stopped for a repair, so
  there is no writer to cut around.
- The sidecar and coordinator must resume from the restored clone's cut rather than from
  their pre-repair progress. Even a patch discards committed state: the state DB sits ahead
  of the clone by every block between the snapshot and the checkpoint that halted it. The
  checkpoint's position in the ledger sizes that window, not `PollInterval`.
- Replay starts at the snapshot's own block, not the block after it. The restored clone
  holds that block's regular transactions but not its snapshot TX; the regular ones replay
  as duplicates, which the VC already resolves to their original status.
- The repair rolls back the sidecar's block store to the block before the restored clone's
  snapshot block. The block store holds each block with its per-TX statuses, starts from
  its own height (`service/sidecar/block_store.go:71`), and skips any block below it
  (`service/sidecar/block_store.go:90`). Without the rollback, replayed blocks keep their
  pre-repair statuses and the block store disagrees with the repaired state.
  fabric-x-common already has `blkstorage.Rollback`, and `blkstorage.ResetBlockStore` for
  reset.
- The troubleshoot report (R3) must be written before the repair. Rolling back the block
  store discards the pre-repair statuses the report reads.
- Before replay, the repair frees the name `snapshot_<block>` of every clone the replay will
  pass, keeping each clone as evidence under another name. The restored state has no
  `tx_status` row for those snapshot TXs, so the VC treats each re-delivered one as a first
  submission and creates its clone (`service/vc/database_snapshot.go:214`). It reuses any
  database already holding the name (`service/vc/database_snapshot.go:246`). For the
  diverging snapshot, that rehashes the unrepaired clone, and the replayed checkpoint
  halts again on the same mismatch.
- All three: offline admin CLI, run with the committer stopped, idempotent / re-runnable,
  and ending in a hash that matches the endorsed checkpoint. A repair that is not verified
  is not done.
- The blocks needed for replay must be at or after the ledger's pruned start block; fail
  loudly when they have been pruned.

## R5 — Clone lifecycle (dependency, flagged)

Clones are listed and deleted with `committer db clone list` and `committer db clone delete`
(#826, #903). Delete works on the state database directly, and refuses a snapshot that is not
`CHECKPOINTED` or `ABORTED`. No command restores from a clone. Rollback is only "immediate"
while the last `CHECKPOINTED` clone still exists locally, and the admin decides: keep it, back
it up and delete it, or delete it outright. Backup files are produced on demand only — a
joining org or a divergence — not routinely.

What SDLR still needs from the clone lifecycle:

- Documented guidance to keep the last `CHECKPOINTED` clone until the next snapshot is
  checkpointed. `db clone delete` accepts any `CHECKPOINTED` clone, so nothing stops an admin
  from deleting the one rollback needs.
- Tracking for evidence clones. A halt leaves the diverging snapshot's status unchanged, so
  `db clone delete` already refuses its clone. But repair renames clones as evidence, then
  restores, which discards the `_snapshot` records that named them. `db clone delete` only
  deletes a clone some record names, so the repair command must record its evidence clones
  and be the one that deletes them.

## Decisions taken, and what they rule out

- **Compare is always two-sided.** A correct checkpoint hash needs no proof; an incorrect
  one starts with the admin reaching out to the other orgs. Rules out a radix/MPT and any
  standalone inclusion-proof format.
- **One flat level of buckets, not a deep Merkle tree.** Interior levels buy only wire
  bytes, already negligible at ~2 MB per table for K=16, and pay for them in round trips
  and rescans — or, if persisted per row, 32 GB on a 1B-row table.
- **Buckets keyed by key prefix, not by row position or by `H(key)`.** Positional leaves
  shift wholesale when one row is missing, which is the case this exists to find. Hashed
  key bits are perfectly uniform but turn each narrowing rescan into a filtered full scan
  instead of the index seek the hasher already issues.
- **K from the snapshot TX, not from configuration.** The TX is ordered *and* endorsed
  (`/Channel/Application/SnapshotEndorsement`, `service/verifier/policy/policy.go:56`), so
  every org reads an identical K by construction, whereas config drift between orgs stays
  silent until an incident — and a K bound that differs between orgs forks them on a TX one
  accepts and the other rejects.
- **Digests persisted, not recomputed when compare runs.** Two-sided compare could
  recompute them from the immutable clones, but that is a full rescan per side, 22–55 min
  on a 1B-row table, at the moment the committer is halted and two orgs are on a call.
- **The root hash is re-derived rather than held byte-identical.** Determinism was the
  actual requirement. Folding every row a second time to preserve today's encoding measured
  +110% on the hash step; deriving the root from the bucket digests measured within noise.
- **No repair mode is trusted on its own; the checkpoint proves each one.** Patch may receive
  wrong rows from the peer. Rollback and reset replay the ledger through the same code that
  diverged, so an unfixed bug, or a divergence that did not come from the ledger, reproduces
  the same wrong state. All three end by matching the checkpoint the endorsers signed, and a
  failed match halts again. Replaying the ledger proves nothing the checkpoint does not, so
  the modes differ only in cost: how much they replay, and whether they need a peer.
- **The format version guards R4 across time, not across orgs.** A rollback rehashes an
  older clone, and an algorithm change since must fail loudly rather than read as a
  divergence.

## Out of scope

- Block-store / chain-hash verification (`ledgerutil verify`) — agreed it can come later.
- Pruning, bootstrapping, external TX indexing — other tasks / EPICs.

## Open questions to settle before design

1. How does the tool obtain the peer's digests — a committer API, or strictly out-of-band
   backup files? This decides whether R2 is a pure CLI or a service change. Two-sided
   compare puts a human in the loop while the committer is halted, which argues for a file.
2. Which DB structure holds the digests? R1.5 settles that they are in the state DB. A
   new table keyed by (snapshot, table, bucket) needs a `listHashedTables` exclusion and
   writes up to 2^K rows per table per snapshot. A blob on the `_snapshot` record avoids
   the table, but `SnapshotStateManager.List` decodes every record (`committer db clone
   list`), and the scheduler reads the latest record on every poll, so ~100 MB per record
   would be read far more often than it is used.
3. Does rollback need coordinated multi-org action, or is each org independent once the bug
   is fixed?
