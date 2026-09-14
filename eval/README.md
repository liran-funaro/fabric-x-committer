<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Evaluation

Measurements taken on a cluster of nineteen machines, and of thirty-nine once a real ordering service is
in the path, the configuration behind them, and the apparatus that produced them. Everything here describes one deployment on particular hardware on particular days; it is
not product documentation, which is what [`../docs/`](../docs/) holds. The general guide to what each
setting does is [`../docs/performance-tuning.md`](../docs/performance-tuning.md) — these notes say what
actually moved.

## Which document a thing goes in

Each document has one job, and the second column is as binding as the first. A fact in the wrong document
is worse than a fact left out: the publication picks up methodology it should not carry, and the task list
becomes something nobody can act on. **If in doubt it goes in
[`cluster-optimization-log.md`](cluster-optimization-log.md)** — that is the only document with no exclusions.

| document | holds | never holds |
|---|---|---|
| [`evaluation.tex`](evaluation.tex) | **results only.** What the deployment achieves: throughput, latency, CPU, what each setting is worth, and the setup needed to read them | how a number was arrived at, what went wrong on the way, retracted claims, instrument defects, anything unresolved, anything about the harness |
| [`eval-todo.md`](eval-todo.md) | **tasks only.** What still needs running or writing, in priority order, with the one line each needs to be actionable | findings, result tables, measurements, narrative, anything already done |
| [`cluster-optimization-log.md`](cluster-optimization-log.md) | **every finding, accumulated, including the dead ends.** Evidence, retractions, refuted mechanisms, instrument defects, and what each was worth | nothing — this is the record, and a wrong turn removed from it will be taken again |
| [`optimization-summary.md`](optimization-summary.md) | **the optimizations worth preserving,** summarised for someone who will apply them and was not here | the reasoning that produced them, or anything superseded |
| [`optimization-config.md`](optimization-config.md) | the assembled configuration that produced the figures, parameter by parameter | why a parameter has its value |
| [`optimization-issues.md`](optimization-issues.md) | the issues the evaluation filed, and the ones it still needs to | anything not destined for a tracker |
| [`paper-figures.md`](paper-figures.md) | the Fabric-X paper's committer figures recreated, and the caveats specific to comparing against a published number | findings that are not about the comparison |
| [`RUNNING.md`](RUNNING.md) | how to run every experiment: which arm, how to bring it up, what counts as a measured point, how the figures and the PDF are produced | results |
| [`session-handoff.md`](session-handoff.md) | what an incoming session needs that is true only right now: what is running, what is half-done, which clock the logs use | anything durable, which belongs in the log |

Nothing else belongs in this directory. A draft of something that has a home is a second copy of it, and the
copies diverge: `abstract.md` was a draft of the abstract now inside `evaluation.tex`, and by the time anyone
compared them it said the committer tier was eighteen machines against the inventory's nineteen. Draft in
place.

Two consequences worth stating, because both were got wrong:

- A measurement that has been **retracted** stays in the log with its refutation, and comes out of the
  publication, the summary and the task list entirely.
- A **result** goes in the publication; the *check* that established it goes in the log. "Fully retired at a
  flat in-flight count" is a result. "Which of two counting methods was right" is not.

## Figures

![Committer throughput and tail latency](figures/figure9.png)

![What latency the committer costs at a given throughput, and what the block size trades](figures/latency-throughput.png)

[`figures/figures-table.md`](figures/figures-table.md) is every reported point with its rate limit,
abort rate, latencies, database commit latency, table fill and per-tier CPU.

The figures above are the PNG copies, because GitHub will not render a PDF inline. The PDF beside each
one is what [`evaluation.tex`](evaluation.tex) includes: vector, so it survives being scaled into a
column and its text stays selectable.

## Apparatus and data

## Two arms

Every figure above measures the **committer only**: the load generator embeds a mock orderer, cuts and
signs the blocks itself, and serves them to the sidecar. That is what the paper's Section 6.3 does, so
it is the comparable measurement. The second arm puts a real ordering service in the path — 4 parties,
4 shards, sixteen batchers two to a machine over 20 machines — and measures what the whole pipeline
costs at a given rate, into `figures-orderer.jsonl` and `figures/e2e/`. That arm reports the
latency-throughput curve only, not the bar panels.

The paper has no end-to-end figure to compare against. It publishes ordering alone (430,000 tps at 4
parties and 4 shards, Figure 7a; 414,000 at two) and the committer alone (419,000–474,000 tps, Figure 9), and those two
bracket what an end-to-end number can be.

`figures.jsonl` is the raw output: one JSON object per probe and per hold, including the measurements
that failed and the ones later retracted, so any figure here can be rebuilt or disputed from the same
data. `graph.jsonl` is the dependency-graph and database sampler's 30-second series over the same runs.

| script | what it does |
|---|---|
| `scripts/fx-figures.py` | the driver: the experiment matrix, a fresh deployment per point, the rate search and the confirmation hold |
| `scripts/fx-bringup.sh` | brings an arm up in the only order that works — stop, wipe, setup, gate on the crypto, start, init, gate on a committed rate — with `INV` selecting the arm |
| `scripts/fx-run-matrix.sh` | the outer loop: refuses a second driver, brings the arm up, proves it is the arm asked for from the running generator's own config, then runs the driver over a set of experiments |
| `scripts/fx-plot-figures.py` | reads `figures.jsonl` and writes the two figures — PDF for `evaluation.tex`, PNG for the embeds above — and the table |
| `scripts/fx-graph-sampler.py` | samples the dependency graph, the database and per-machine CPU every 30 s alongside a run |
| `scripts/fx-join-graph.py` | joins the sampler's series to each confirmed hold |
| `scripts/fx-fill-test.py` | the within-hold test of whether table size costs commit latency |
| `scripts/fx-disk-bench.sh` | characterises a node's disk with `fio`, in the four patterns this deployment produces; runs on the unused second disk so nothing live is touched |
| `scripts/fx-build-pdf.sh` | builds `evaluation.pdf`, twice, and fails on an unresolved cross-reference |

Regenerate the figures from the data:

```sh
eval/scripts/fx-plot-figures.py eval/figures.jsonl eval/figures/   # the committer panels and curve
eval/scripts/fx-plot-e2e.sh                                        # the end-to-end figures
eval/scripts/fx-build-pdf.sh                                       # evaluation.pdf
```

To run the experiments rather than redraw them, see [`RUNNING.md`](RUNNING.md).

The driver and the sampler run on the evaluation cluster's control node and read from its Prometheus;
they are here so the figures are reproducible and the measurement rules are inspectable, not because
they run anywhere else. The cluster bundle they belong to (inventory, playbooks, the rest of the
tooling) is separate.
