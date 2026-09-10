<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Running the evaluation

Every experiment behind [`evaluation.tex`](evaluation.tex) — every figure, both tables, and the claims in the text. The
experiments run on a 39-machine cluster from a control node; nothing here runs on a workstation
except the plotting and the PDF.

**Read this first.** Edit and commit here, then sync one way to the control node. Never edit on the
control node: it has no git remote and a change made there is lost at the next sync.

```bash
rsync -a eval/scripts/ monitor:/data1/logs/
```

## What produces what

| In the document | Experiment ids | Arm |
|---|---|---|
| Fig. 1a, throughput and tail latency against transaction size | `9a-rw1` … `9a-rw4` | committer |
| Fig. 1b, against invalid-signature share | `9b-inv0` … `9b-inv30` | committer |
| Fig. 1c, against double-spend share | `9c-ds0` … `9c-ds30` | committer |
| Fig. 2, latency against throughput at two block sizes | `curve`, `curve500`, `curve500hi`, `curve500top` | committer |
| Fig. 3, end to end by shard count and storage | `e2e-shape-4s`, `e2e-shape-4s-hi`, `e2e-shape-8s`, `e2e-shape-8s-hi` | end-to-end |
| Fig. 4, end to end by batch size | `e2e-curve-small` against `e2e-shape-4s` | end-to-end |
| Fig. 5 and Table 1, throughput against transaction size | `e2e-size300` … `e2e-size4096` | end-to-end |
| Table 2, storage characterisation | `fx-disk-bench.sh` | either |
| The 420 ns per key in §Throughput and tail latency | `fx-graph-sampler.py`, `fx-join-graph.py` | committer |
| The fill statement in §Threats to validity | `fx-fill-test.py` | end-to-end |

Ids not in that table are diagnostics rather than figures: `gdg-*` (the global dependency graph
against the simple manager), `split0-*` (the same conflict workload at the default tablet split),
`chunk-rw4`, `rung-*`, `9a-utxo*`, `e2e-fresh-*`.

## Prerequisites

1. **The cluster bundle.** `~/workspace/fx-cluster` synced to `monitor:/data1/cluster`, holding the
   three inventories and `bin/fx-env.sh`, which every script sources for `$FX_PROJECT`.
2. **Staged binaries.** `committer` and `loadgen` built for linux/amd64 and copied to
   `/data1/bin-stage/` on the control node. `committer_build_bin` is false, and a `setup` run empties
   the collection's `out/` tree before the transfer play reads it, so the bring-up restores them from
   there on every pass. Build them here, not on the cluster: the workers have no Go and no internet.
3. **Monitoring up.** Prometheus on `https://localhost:9090` of the control node. Every measurement is
   a Prometheus query; the driver cannot measure anything without it.

## Step 1 — pick an arm

| Inventory | What it deploys |
|---|---|
| `inventory/cluster.yaml` | 19 machines, committer only, mock ordering service inside the load generator |
| `inventory/cluster-orderer.yaml` | those 19 plus 20 more: a real Arma service, four parties, four shards |
| `inventory/cluster-orderer-8shard.yaml` | the same 39 at eight shards, two batchers per volume |

The two arms cannot be up at once, and switching between them is a full bring-up, not a restart.

## Step 2 — bring it up

```bash
ssh monitor
cd /data1/logs
INV=/data1/cluster/inventory/cluster.yaml nohup ./fx-bringup.sh > bringup.log 2>&1 &
```

The order is stop → wipe → setup → gate on crypto → start → init → gate on a committed rate, and it
is not negotiable — `make start` starts what is not running and repairs nothing, so any other order
leaves live processes holding pre-wipe state. `fx-bringup.sh` explains each step it takes and why.

Three gates decide whether it worked, and each checks an artifact rather than an exit code: no
database state on any host, every certificate in every org tree verifying against its CA, and a
non-zero committed rate with a latency series present. Block height is not a gate — a run rejecting
100% of transactions raises it happily.

## Step 3 — run experiments

`fx-run-matrix.sh` does the bring-up, proves the arm is the one asked for, and then measures:

```bash
# Fig. 1 and Fig. 2, the whole committer matrix
TAG=committer ONLY=9a-,9b-,9c-,curve OUT=/data1/logs/figures.jsonl \
  nohup ./fx-run-matrix.sh > committer.log 2>&1 &

# Fig. 3, four shards then eight — two inventories, so two runs
TAG=e2e-4s E2E=1 ONLY=e2e-shape-4s \
  INV=/data1/cluster/inventory/cluster-orderer.yaml        nohup ./fx-run-matrix.sh > e2e-4s.log 2>&1 &
TAG=e2e-8s E2E=1 ONLY=e2e-shape-8s \
  INV=/data1/cluster/inventory/cluster-orderer-8shard.yaml nohup ./fx-run-matrix.sh > e2e-8s.log 2>&1 &

# Fig. 4 and Fig. 5
TAG=e2e-batch E2E=1 ONLY=e2e-curve-small nohup ./fx-run-matrix.sh > e2e-batch.log 2>&1 &
TAG=e2e-size  E2E=1 ONLY=e2e-size        nohup ./fx-run-matrix.sh > e2e-size.log  2>&1 &
```

Run **one** at a time. Two drivers on one cluster is the failure that costs a whole night: both call
`make limit-rate`, their plays collide, every rate-set returns rc=2, and the matrix reports a
completed run having measured nothing. The script refuses to start a second, so trust its refusal.

`ONLY` matters beyond saving time. The end-to-end stages need different inventories and different
shared configs, so they cannot share a deployment; without `ONLY` the driver would measure one shape
while reporting a matrix.

Results append as JSON lines, one file per arm and scheme, because experiment ids repeat across them:

| File | What is in it |
|---|---|
| `figures.jsonl` | the committer arm under Ed25519 |
| `figures-ecdsa.jsonl` | the committer arm under ECDSA |
| `figures-orderer.jsonl` | the end-to-end arm |

## Step 4 — what a measured point is

Each experiment searches for the highest rate that qualifies, in `1.08` steps, then confirms it.
A point qualifies when it arrived in full (within 2%), committed within 2%, kept p99 under one
second, and held a flat in-flight count.

- A **probe** holds a rate for 90 s after a 75 s settle.
- A **hold** repeats it for 300 s on a fresh deployment. A probe is routinely optimistic — rates have
  passed a probe and lost the hold — so a hold is quoted in preference to a higher probe.
- Between points the rate is parked low until the in-flight count stops falling, because a probe above
  the knee leaves millions of transactions queued and the next point would report their drain time as
  its latency.

Knobs, all optional: `FX_HOLD`, `FX_SETTLE`, `FX_WINDOW`, `FX_SLO_P99`, `FX_TOLERANCE`, `FX_SEED`,
`FX_UP_STEPS`, `FX_SKIP_DEPLOY=1` (measure whatever is running, refusing any experiment whose shape does
not match), `HOURS` (deadline, checked before each experiment).

**To re-measure a point that already holds, pass `REDO=<ids>` to the runner** — not `FX_REDO`, which the
runner overrides with an empty string. Getting that wrong is silent: the run skips every experiment it
was meant to re-measure and still finishes with `MATRIX COMPLETE`. The runner echoes `redo=` on its
matrix line, so check that line before walking away.

Each run writes two logs: `<TAG>.log` for the runner's own gates and `<TAG>-driver.log` for the
measurements.

**Never restart the load generator mid-run.** With the mock orderer, restarting any pipeline component
freezes the sidecar's ledger for good. Change configuration before `make start`, not after.

## Step 5 — the tables

```bash
./fx-disk-bench.sh                  # Table 2: fio, four patterns, on an unused volume
python3 fx-fill-test.py             # the fill statement in Threats to validity
python3 fx-graph-sampler.py         # samples the dependency graph while a run is up
python3 fx-join-graph.py            # joins those samples to the rate ladder
```

## Step 6 — figures and the PDF

On this workstation, not the control node:

```bash
cd eval
rsync -a monitor:/data1/logs/figures.jsonl .
python3 scripts/fx-plot-figures.py figures.jsonl figures/        # Fig. 1, Fig. 2
scripts/fx-plot-ecdsa.sh                                        # the same, from the ECDSA run
scripts/fx-plot-e2e.sh                                          # Fig. 3, Fig. 4, Fig. 5
scripts/fx-build-pdf.sh                                         # evaluation.pdf
```

Each plot script also writes `figures-table.md` beside its figures: every point it drew, with the
rate, the tail, the fill it ran against and the busiest host. Read a number off that rather than off
a figure.

`fx-build-pdf.sh` runs pdflatex twice and fails on an unresolved reference — a single pass leaves `??`
in the text and still exits 0.

## Adding an experiment

Add one `dict` to `EXPERIMENTS` (committer) or `E2E_EXPERIMENTS` (end to end) in `fx-figures.py`:

```python
dict(id="9c-ds5", figure="9c", x=5, label="5% double spend", seed=BASE_SEED,
     vars=shape(2, 0, backref=0.05)),
```

`id` is what `ONLY` matches, `figure` and `x` are where the point lands in a plot, `seed` is the rate
the search starts from, and `vars` are inventory variables for this point only. `shape()` builds the
workload: read-write count, blind-write count, invalid-signature share, back-reference rate (the
double-spend share), block size. Then teach `fx-plot-figures.py` to draw the new `figure`, if it is
not one it already draws.
