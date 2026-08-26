<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->

# Optimization configuration: the setup that produced the numbers

The assembled configuration behind the figures in `optimization-summary.md` — every deviation from a
default, in one place, so a run can be reproduced rather than reconstructed from a change log.

Three companion documents, and what each is for:

| Document | Answers |
|---|---|
| this one | *what was configured* |
| [`performance-tuning.md`](performance-tuning.md) | *what each setting does*, and how to tune it on other hardware |
| [`optimization-summary.md`](optimization-summary.md) | *what each change was worth* |
| [`cluster-optimization-log.md`](cluster-optimization-log.md) | *how it was found*, with the evidence and the retractions |

These values are **not** proposed as repository defaults. They are one deployment's answer on one
class of hardware, for one workload shape. The workload in particular is load-bearing — see
[What this does not measure](#what-this-does-not-measure) before quoting any of it.

## What it produces

| | |
|---|---|
| Sustained, 300 s window, fresh deployment | **500,000 tps** at 99.9% success, ~392 ms mean end-to-end latency |
| Over-driven mean | **578,383 tps** |
| Over-driven peak | **590,400 tps** |
| Four-hour soak | 500,000 tps held for 3 hours at ~315 ms, then decayed to ~450,000 as database compaction saturated the disks |
| Binding resource at that rate | the **database** — 83% CPU, 88% disk busy, ~21x write amplification. Every committer stage has headroom. |

## Deployment shape

Nineteen machines, each **64 cores and 156 GB**, every process a host binary — no containers, no
Kubernetes.

```
loadgen (embedded mock orderer)
  -> sidecar  x1
  -> coordinator  x1
  -> signature verifiers  x3
  -> validator-committers  x6
       -> YugabyteDB: 3 masters, 12 tablet servers
```

Placement details that matter:

- **No ordering service.** The load generator cuts and signs blocks itself and serves them over the
  Atomic Broadcast API, so nothing measured belongs to an orderer. This is what the loadgen's
  `sidecar-client` section selects.
- **Six of the twelve database machines also host a validator-committer.** The other six VCs are on
  their own machines. Co-location saves a network hop on the busiest link and did not measurably cost
  anything, because the VC is not CPU-bound at these rates.
- **Two data directories per tablet server** (`/data1/yb-tserver`, `/data2/yb-tserver`), on separate
  devices. Disk is what eventually binds, so this is not incidental.
- One monitoring host running Prometheus and Grafana. Every number in these documents comes from the
  services' own metrics, not from external instrumentation.
- mTLS between committer components; TLS to YugabyteDB.

## Committer configuration

Only the settings that differ from their default. Everything else is stock.

### Sidecar

| Key | Value | Default | Why |
|---|---|---|---|
| `ledger.disable-tx-id-index` | `true` | `false` | The txID index writes one LevelDB entry per **transaction**, and its compaction was 35% of sidecar CPU and growing with the ledger. Worth 115,200 → 297,200 tps. Selects the on-disk format, so it can only be changed on an empty ledger. |
| `channel-buffer-size` | `5` | `100` | The buffer held 237 submitted blocks whose statuses had not returned — pure queueing delay. Latency 6,355 → 1,156 ms at unchanged throughput. |
| `waiting-txs-limit` | `500000` | `20000000` | The in-flight window. **Must be ≥ the coordinator's `waiting-txs-limit`**; below it, this silently becomes the binding window instead of the coordinator's and caps throughput. |

### Coordinator

| Key | Value | Default | Why |
|---|---|---|---|
| `dependency-graph.use-simple-manager` | `true` | `false` | The default manager's graph stages read as 95% busy with 40% of that in lock wait, on a machine under 20% per-thread. Worth +47.6% (329,854 → 486,941 tps) — under the default manager the *database* is starved, at 62 ms batch commit and 60% CPU. |
| `dependency-graph.waiting-txs-limit` | `500000` | `20000000` | Bounds coordinator memory: 79 GB → 786 MB at unchanged throughput. This is the window Little's law applies to, so it and the delivered rate together set the latency. |

`dependency-graph.num-of-local-dep-constructors` is left at its default and has **no effect** while
`use-simple-manager` is true.

### Validator-committer (all six, identically)

| Key | Value | Default | Why |
|---|---|---|---|
| `resource-limits.max-workers-for-preparer` | `16` | `1` | Preparer is cheap; 16 leaves it permanently idle (4 of 96 workers busy at 500,000 tps), which is the point. |
| `resource-limits.max-workers-for-validator` | `16` | `1` | Same. |
| `resource-limits.max-workers-for-committer` | `64` | `20` | The commit stage is where the database round trips happen, so this is the pipeline's concurrency into the database: 6 x 64 = 384 concurrent batches. Worth +17% over 40 workers. |
| `database.max-connections` | `128` | `10` | Must exceed the committer worker count or the workers queue on the pool rather than on the database. |
| `database.min-connections` | `16` | `5` | Avoids connection setup cost in the ramp. |
| `database.table-pre-split-tablets` | `120` | one per tablet server | Ten tablets per tablet server, for write concurrency. **+35% on this workload**, and a factor-of-24 cost on any multi-key read — see the warning below. |

### All services

gRPC HTTP/2 flow control needs **no configuration**: since #790 the recommended windows — 16 MiB per
stream, 32 MiB per connection — are what an unset `flow-control` section resolves to, on both clients
and servers. That is the configuration these numbers were measured with. It is worth +13.3% over
gRPC's own defaults, at 39% lower latency.

Override per client or per server only to depart from it:

```yaml
flow-control:
  initial-window-size: 16777216       # per stream;     negative = leave gRPC's BDP tuning alone
  initial-conn-window-size: 33554432  # per connection; must exceed the stream window
```

## Database configuration

| Setting | Value | Why |
|---|---|---|
| State table pre-split | `SPLIT INTO 120 TABLETS` | ten per tablet server; see the warning below |
| `--ysql_max_connections` | `500` | 6 VCs x 128 connections needs headroom above the default |
| Data directories | two per tablet server, separate devices | disk is the binding resource at these rates |
| TLS certificate SANs | **every cluster node in every certificate** | without this, the smart driver's topology refresh fails and *every* connection pins to one tablet server. A real defect, worth no throughput on its own, but it invalidates any measurement taken with it. |
| `load-balance` (committer side) | `true` | the default; called out because it is half of the fix above and useless without the SANs |

> [!WARNING]
> **The 120-tablet pre-split is a trade-off, not a free win.** It is worth +35% here (486,941 against
> 359,866 tps at 12 tablets) and every headline figure was measured with it on. Its cost appears only
> when a transaction performs a multi-key lookup (`WHERE key = ANY($1)`): at 120 tablets that issues
> one storage read request **per key** instead of one per tablet, and a blind-write workload commits
> 13,160 tps against 314,336 at 12 tablets — a factor of 24.
>
> What governs the cliff is the **product of tablets and keys per lookup**, which must stay under
> roughly 32,768 on this hardware. So the durable lever is the committed batch width, and the tablet
> count is not a lever at all: YugabyteDB splits as a table grows, and `ns_0` went from 120 tablets to
> 288 over eleven hours of load. Lowering the initial count postpones the cliff rather than removing
> it.

## Load generator: the measurement instrument

The generator is part of the setup, not an accessory. It capped every run near 325,000 tps before
these settings, and the committer was blamed for it.

| Key | Value | Default | Why |
|---|---|---|---|
| `load-profile.block.max-size` | `10000` | `500` | Two sidecar costs are per **block**, not per transaction. Block signature verification alone goes 169,000 → 1,281,000 tx/s. |
| `load-profile.workers` | `128` | one per core | Generation plateaus, and where it plateaus moves with core count — measure it on the machine that will run it. |
| `load-profile.policy` … `key.scheme` | `EDDSA` | `ECDSA` | 325,600 → 374,400 tps. This lifted the **generator's** ceiling, not the committer's. The win is allocation, not entropy: 184 B and 4 objects per signature against ECDSA's 6,067 B and 59. Ed25519 reads no entropy at all. |
| `stream.gen-batch` | `4096` | `1` | 495,583 → 629,884 tx/s generated. 16,384 is faster still but costs four seconds of startup dead time. |
| `stream.buffers-size` | `100` | `1` | Keeps the submit path fed across the batch boundary. |
| `sidecar-client.out-block-capacity` | `100` blocks | unbounded | **Without a bound, an overloaded committer is absorbed rather than felt**: the submitted rate stays at the requested rate while only the committed rate shows the real drain, and latency grows past what the histogram can represent. Keep it a small multiple of the sidecar's `waiting-txs-limit`. |
| `monitoring.latency.max-tracked-txs` | `100000` | `10000` | An undersized table drops samples, biased toward slow transactions, so the reported mean is pulled *down*. Above ~100,000 in flight, stop trusting it and use in-flight ÷ throughput. |
| `limit.rate-limit` | the offered rate | — | Set per run. 500,000 is the sustainable rate; over-driven runs go above it. |

### Workload shape

```yaml
load-profile:
  transaction:
    key-size: 32
    read-write-count: 2
    read-write-value-size: 32
    key-backref-rate: 0     # every key is fresh and unique
```

Two read-write operations on fresh 32-byte keys per transaction, so the committer writes one
`tx_status` row and two state rows per transaction.

## What this does not measure

`key-backref-rate: 0` means **no two transactions ever touch the same key**. So:

- The dependency graph tracks transactions that cannot conflict. This is the case that favours the
  simple manager, and the +47.6% should not be assumed to hold under contention.
- The MVCC validator never aborts anything.
- Nothing performs a multi-key read, which is exactly why the 120-tablet pre-split looks free.

Measured separately, once the tablet cliff was out of the way, write contention costs about **19%**
(254,221 tps at `key-backref-rate` 0.5 against 314,336 at 0, on 12 tablets). Read-write contention was
not measured at all: it needs `queries-rate` above 0 and a query service to serve it, which this
deployment does not run. An attempt to measure it without one produced a workload that was *impossible*
in half its transactions, and what got measured was the failure path.

## Settings deliberately left alone

| Setting | Left at | Why |
|---|---|---|
| `sidecar.ledger.sync-interval` | default | never measured as binding once the txID index was off |
| `dependency-graph.num-of-local-dep-constructors` | default | no effect under the simple manager; and swept 1–32 with no trend under the default one |
| `GOGC` | default | raising it helps the sidecar benchmark substantially but was not applied on the cluster, so no cluster figure here depends on it |
| `server.max-concurrent-streams`, `rate-limit` | default | the client-facing bounds; nothing in this workload approaches them |

## Changes that were tried and reverted

Recorded so they are not retried:

| Change | Result |
|---|---|
| VC committer workers 64 → 40 | **−14.7%**. Predicted −3.7% from a bad comparison; two configurations must be compared at each one's own ceiling, not at a rate both can serve. |
| `pgx.Batch` for the commit's round trips (#307) | ~0.4%. A batch still executes sequentially server-side, so it removes network round trips and not database work — and of the 264 ms commit, 225 ms is execution and queueing inside YugabyteDB. |
| Storing `tx_status` transaction IDs as bytes instead of 64-char hex | ~9% of the binding resource, but unsafe: transaction IDs are arbitrary strings, the snapshot digest covers `tx_status`, and existing rows would need migration. |
| Dropping the pre-split to 12 tablets | a 23x gain on multi-key reads that would have cost a third of every headline figure. Nearly shipped as a pure win. |

## Reproducing a figure

Four rules, each of which produced a wrong number before it was understood:

1. **Quote a 300-second window from a fresh deployment.** The same configuration at the same requested
   rate gives 499,356 over 60 s, 486,941 over 300 s, and 462,296 over 300 s in a run already pushed
   past the knee.
2. **Never quote a run that follows an over-driven one.** Backlog drains no faster than the committed
   rate, so a step after an overloaded step reads low. A 60-second settle does not cover it.
3. **A/B on the same day.** This cluster's baseline moves ~10–15% between days. Rebuild the old code
   and measure both arms today; a comparison against yesterday's number is not a comparison.
4. **Watch the queue gauges, not machine CPU.** A saturated single goroutine is 1.6% of 64 cores. The
   per-block duty cycle — stage latency against the per-block budget — is the diagnostic;
   `performance-tuning.md` §2 maps each queue to its stage.
