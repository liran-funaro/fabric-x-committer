<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Evaluation

Measurements taken on a nineteen-machine cluster, the configuration behind them, and the apparatus that
produced them. Everything here describes one deployment on particular hardware on particular days; it is
not product documentation, which is what [`../docs/`](../docs/) holds. The general guide to what each
setting does is [`../docs/performance-tuning.md`](../docs/performance-tuning.md) — these notes say what
actually moved.

| document | what it holds |
|---|---|
| [`cluster-optimization-log.md`](cluster-optimization-log.md) | how the deployment went from 80,000 to 500,000 tps sustained, what the evidence for each change was, and which changes bought nothing |
| [`optimization-config.md`](optimization-config.md) | the assembled configuration that produced the figures, parameter by parameter |
| [`optimization-summary.md`](optimization-summary.md) | what each change was worth |
| [`optimization-issues.md`](optimization-issues.md) | the issues the evaluation filed, and the ones it still needs to |
| [`paper-figures.md`](paper-figures.md) | the Fabric-X paper's committer figures recreated, with every caveat and confound found in doing it |

## Figures

![Committer throughput and tail latency](figures/figure9.png)

![What latency the committer costs at a given throughput, and what the block size trades](figures/latency-throughput.png)

[`figures/figures-table.md`](figures/figures-table.md) is every reported point with its rate limit,
abort rate, latencies, database commit latency, table fill and per-tier CPU.

## Apparatus and data

## Two arms

Every figure above measures the **committer only**: the load generator embeds a mock orderer, cuts and
signs the blocks itself, and serves them to the sidecar. That is what the paper's Section 6.3 does, so
it is the comparable measurement. The second arm puts a real ordering service in the path — 4 parties,
2 shards, one component per machine over 20 machines — and measures the same panels end to end, into
`figures-orderer.jsonl` and `figures/e2e/`.

The paper has no end-to-end figure to compare against. It publishes ordering alone (414,000 tps at 4
parties and 2 shards, Figure 7a) and the committer alone (419,000–474,000 tps, Figure 9), and those two
bracket what an end-to-end number can be.

`figures.jsonl` is the raw output: one JSON object per probe and per hold, including the measurements
that failed and the ones later retracted, so any figure here can be rebuilt or disputed from the same
data. `graph.jsonl` is the dependency-graph and database sampler's 30-second series over the same runs.

| script | what it does |
|---|---|
| `scripts/fx-figures.py` | the driver: the experiment matrix, a fresh deployment per point, the rate search and the confirmation hold |
| `scripts/fx-figures-run.sh` | switches the cluster from the real-orderer arm to the committer-only arm, then runs the driver |
| `scripts/fx-figures-e2e-run.sh` | switches the other way — a real Arma ordering service in the path — smoke-checks it, then measures the same panels end to end |
| `scripts/fx-plot-figures.py` | reads `figures.jsonl` and writes the two figures and the table |
| `scripts/fx-graph-sampler.py` | samples the dependency graph, the database and per-machine CPU every 30 s alongside a run |
| `scripts/fx-join-graph.py` | joins the sampler's series to each confirmed hold |
| `scripts/fx-fill-test.py` | the within-hold test of whether table size costs commit latency |

Regenerate the figures from the data:

```sh
eval/scripts/fx-plot-figures.py eval/figures.jsonl eval/figures/
```

The driver and the sampler run on the evaluation cluster's control node and read from its Prometheus;
they are here so the figures are reproducible and the measurement rules are inspectable, not because
they run anywhere else. The cluster bundle they belong to (inventory, playbooks, the rest of the
tooling) is separate.
