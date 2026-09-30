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
  `service/snapshothasher/hasher.go:87`.
- Hashed set (`listHashedTables`): every user namespace `ns_<id>` from `ns__meta`, plus
  `ns__config`, `ns__meta`, `tx_status`, `ns__checkpoint`. `metadata` and `ns__snapshot`
  are excluded. Rows are folded in primary-key order as `len(k)||k||len(v)||v`, plus an 8-byte
  big-endian version for namespace rows; table
  digests are combined in a fixed order (fixed system tables, then `ns__meta` registry
  order).

So detection exists. What is missing is **localization** and **repair**.

## R1 — Localization data (extend, do not replace)

1. The hash job persists three levels of digest for every hashed table: the root hash as
   today, one digest per table, and one digest per key-prefix bucket. Two orgs then rule
   out whole namespaces table-by-table, narrow to a bucket, and exchange rows only for
   that bucket.
2. A bucket is a contiguous key range — the rows whose key shares its first K bits, so a
   table holds at most 2^K buckets. The bucket level is flat: a bucket digest is compared
   directly and never descended into.
3. Each row is folded exactly once, into its bucket. The per-table digest is derived from
   its bucket digests and the root from the per-table digests, which is why the
   localization data measures within noise of today's single fold.
4. K is carried by the snapshot TX. The `_snapshot` namespace, marker-only today
   (`Status_MALFORMED_SNAPSHOT_NOT_MARKER_ONLY`, `service/sidecar/mapping.go:507`), gains
   exactly one write holding K, shaped like the `_checkpoint` case one branch below; the
   preparer (`service/vc/preparer.go:192`) copies it into the `SnapshotState` it
   synthesizes. K's permitted range is a constant in code, and a submitter may size K
   within that range from its own row-count estimate.
5. Every persisted digest set carries K and a format version in its header, so a clone or a
   backup file is interpretable on its own. The clone is created before its own `_snapshot`
   record commits (`service/vc/database_snapshot.go:245`), so K is reachable nowhere else.
6. The persisted digests are excluded from the hashed set and are deleted with their clone.
   Hashing them would fold each snapshot's digest into the next; tying them to the clone
   puts them under R5's clone-deletion API instead of a second retention policy.
7. Digests are deterministic and content-only: identical content ⇒ identical digests,
   regardless of table-completion order, page size, worker count, or node count.
8. Digests are recomputable offline by a tool with no running committer, from a clone, a
   backup file, or the state DB itself — the committer is halted exactly when this is
   needed, and with no writer there is no cut to take.
9. Computing the digests must not disturb a live cluster: reuse the existing bounded keyset
   pagination and worker limits, adding no concurrent read load beyond the configured
   limits.

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
- Human-readable, file-based report — no service, no API. Operators run this once, under
  pressure.

## R4 — Repair tools

Every repair restores the state DB from a clone and re-consumes the ledger from that clone's
cut. The modes differ in which clone, and in what happens before the replay.

- **Rollback**: restore from the previous validated clone, then re-consume every block since
  its cut. Needs no diff and no peer — the ledger is authoritative and the bug is presumed
  fixed before the replay — at the cost of re-consuming a full snapshot interval of load.
- **Patch**: restore from the latest (diverging) clone, replace the buckets R2 identified
  with the peer's rows, verify, and only then re-consume the blocks committed after that
  clone's cut. The replay window is bounded by how late the hasher noticed the snapshot
  rather than by the snapshot interval.
- **Reset**: drop the state DB and rebuild by replaying the ledger from genesis (or from
  the bootstrap snapshot).
- The repair unit is the bucket, not the row. A divergent bucket's whole key range is
  replaced by the peer's rows for that range, which handles missing and extra rows without
  per-row diff semantics and makes the set of buckets to re-validate exactly the set
  repaired.
- A patch verifies before it replays. Recompute the digests of the repaired buckets, re-fold
  them with R1's persisted digests for every untouched bucket into the table digest and the
  root, and confirm the root now matches the peer's — all at the clone's cut, which is the
  only height where the comparison is defined. Replaying forward first leaves nothing to
  compare against.
- Rollback and reset re-validate at the next snapshot, by full rehash. Both change every
  table below the restored height, so no persisted bucket digest survives them.
- Re-validation reads the repaired state DB directly, taking no fresh clone. A clone exists
  to give a consistent cut of a live cluster; the committer is stopped for a repair, so
  there is no writer to cut around.
- The sidecar and coordinator must resume from the restored clone's cut rather than from
  their pre-repair progress. Even a patch discards committed state: the state DB sits ahead
  of the clone by however long the hasher took to notice the snapshot (`PollInterval`,
  default 1m — at 100k tps, ~6M transactions).
- All three: offline admin CLI, run with the committer stopped, idempotent / re-runnable,
  and ending in a verified hash. A repair that is not verified is not done.
- The blocks needed for replay must be at or after the ledger's pruned start block; fail
  loudly when they have been pruned.

## R5 — Clone lifecycle (dependency, flagged)

We need an admin API to delete a clone (not in the tree yet), plus documented guidance to
retain the last known-good one. Rollback is only "immediate" while that clone still exists
locally, and the admin is the one who decides: keep it, back it up and delete it, or delete
it outright. Backup files are produced on demand only — a joining org or a divergence — not
routinely.

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
- **Patch is offered, at a cost to reproducibility.** It replays the least, but the state it
  produces is no longer the deterministic result of replaying the ledger, and it presumes a
  peer whose rows are known to be correct — which two orgs alone cannot establish. Rollback
  and reset rebuild from the ledger and carry neither cost.
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
2. Where do the persisted digests live? A table in the source state DB (needs a new
   `listHashedTables` exclusion and writes 2^K rows per table per snapshot), a blob on the
   `_snapshot` record (read on every scheduler poll — too hot), or a file beside the clone
   (tool-friendly, but the hasher is one movable instance, so durability and discovery
   become ours).
3. Does rollback need coordinated multi-org action, or is each org independent once the bug
   is fixed?
