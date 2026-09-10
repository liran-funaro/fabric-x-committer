#!/usr/bin/env bash
# Pull the end-to-end results off the control node and redraw the e2e figures.
# Separate from the committer figures because the two arms measure different systems: the third
# argument switches the taller latency axis (a 500 ms BatchCreationTimeout floor) and the
# ordering-alone reference line.
set -eu
cd "$(dirname "$0")/.." || exit 1
rsync -a monitor:/data1/logs/figures-orderer.jsonl figures-orderer.jsonl
# Two latency figures from one results file: the shard/storage comparison and the block-size
# comparison. Six series on one pair of axes is unreadable, and they answer different questions.
python3 scripts/fx-plot-figures.py figures-orderer.jsonl figures-e2e/ e2e
mkdir -p figures-e2e-batch
python3 scripts/fx-plot-figures.py figures-orderer.jsonl figures-e2e-batch/ e2e-batch
