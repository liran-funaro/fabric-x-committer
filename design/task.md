# Task 3 — Divergence detection & repair (snapshot/checkpoint EPIC, phase 2)

Owner per the phase-2 split with Senthil: Liran takes the divergence check + fix.
Senthil takes pruning (block store) and bootstrapping / backup-of-clone-to-file.

## Context already in the tree

- Divergence is *detected* today. The snapshot hasher (`service/snapshothasher/`, one
  instance alongside any number of VCs, discovering work from the durable `_snapshot`
  record rather than from a notification) hashes the snapshot clone; a mismatch against
  the checkpoint TX's hash produces `CheckpointFeedback.HALT`
  (`api/servicepb/common.proto:48`) with both hashes in `reason`. The committer stops and
  the clone is a frozen, immutable copy of the diverging state.
- Only the **root** hash is persisted (`committerpb.SnapshotState.hash`). Per-table
  digests are computed and discarded — see the explicit phase-2 `NOTE` at
  `service/snapshothasher/hasher.go:85`.
- Hashed set (`listHashedTables`): every user namespace `ns_<id>` from `ns__meta`, plus
  `ns__config`, `ns__meta`, `tx_status`, `ns__checkpoint`. `metadata` and `ns__snapshot`
  are excluded. Rows are folded in primary-key order as `len(k)||k||len(v)||v`; table
  digests are combined in a fixed order (fixed system tables, then `ns__meta` registry
  order).

So detection exists. What is missing is **localization** and **repair**.

## R1 — Localization data (extend, do not replace)

1. Persist the per-table digests next to the root hash, so two orgs compare
   table-by-table first. Must not change the root-hash encoding: `SnapshotState.hash`
   stays byte-identical and existing checkpoints keep verifying.
2. Within a divergent table, narrow to individual rows with a Merkle tree over the rows
   in primary-key order. The requirement is "narrow the diff cheaply", not
   "be Ethereum-compatible": a binary Merkle over the already-PK-ordered pages is the
   lazy form. Pay for a radix/MPT only if we need key-addressed proofs without the peer
   holding the whole table.
3. Deterministic and content-only: identical content ⇒ identical digests, regardless of
   table-completion order, page size, worker count, or node count.
4. Recomputable offline from a clone (or a backup file) by a tool, with no running
   committer — the committer is halted exactly when this is needed.
5. Must not disturb a live cluster: reuse the existing bounded keyset pagination and
   worker limits; no new concurrent read load beyond the configured limits.

## R2 — Compare tool (`ledgerutil compare` analogue)

- Inputs: two snapshot sources at the **same** checkpoint height — the local clone,
  another org's clone, or a backup file. Backup-to-file comes from the bootstrapping
  task; treat it as a dependency, not a deliverable here.
- Exchange digests / subtree hashes, not full state: bandwidth proportional to the
  divergence, not to the database.
- Output both classes, both are needed:
  - **`tx_status` diffs** — txID, status, height. Primary signal: a mishandled TX.
  - **key/value diffs** — namespace, key, value, version. Needed for data corruption.
- Report which tables agree, so whole namespaces can be ruled out immediately.

## R3 — Troubleshoot output

- For each divergent key, name the TX(s) that wrote it and their block / TX position, so
  the manual root-cause hunt starts from a block number and not a hex key.
- Human-readable, file-based report. Operators run this once, under pressure: no service,
  no API.

## R4 — Repair tools

- **Rollback**: restore the state DB from a retained local clone at an earlier known-good
  checkpoint, then replay ledger blocks from that height. Requires the sidecar and
  coordinator to resume from the restored height rather than their pre-rollback progress.
  A local COW clone makes this near-immediate; a backup file is the fallback when no
  clone was kept.
- **Reset**: drop the state DB and rebuild by replaying the ledger from genesis (or from
  the bootstrap snapshot).
- Both: offline admin CLI, run with the committer stopped, idempotent / re-runnable, and
  ending by recomputing the snapshot hash and confirming it matches the peer's. A repair
  that is not verified is not done.
- The rollback target must be at or after the ledger's pruned start block; fail loudly
  when the blocks needed for replay have been pruned.

## R5 — Clone lifecycle (dependency, flagged)

Rollback is only "immediate" while the previous known-good clone still exists locally.
The admin decides: keep the clone, back it up and delete it, or delete it outright. We
need an admin API to delete a clone (not in the tree yet) and documented guidance to
retain the last known-good one. Backup files are produced on demand only — a joining org
or a divergence — not routinely.

## Out of scope

- Block-store / chain-hash verification (`ledgerutil verify`) — agreed it can come later.
- Pruning, bootstrapping, external TX indexing — other tasks / EPICs.

## Open questions to settle before design

1. How does the tool obtain the peer's digests — a committer API, or strictly
   out-of-band backup files? This decides whether R2 is a pure CLI or a service change.
2. MPT vs sorted-row Merkle: is anyone consuming standalone inclusion proofs, or is
   compare always two-sided?
3. Does rollback need coordinated multi-org action, or is each org independent once the
   bug is fixed?
