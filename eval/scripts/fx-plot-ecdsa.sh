#!/usr/bin/env bash
# Redraw the committer figures from the ECDSA re-measurement.
#
# Separate from fx-plot-figures.py's default input because the two schemes share experiment ids: a
# single file would merge Ed25519 and ECDSA rows into one bar. Output goes to figures-ecdsa/ so both
# sets exist side by side until the document has fully moved over.
set -eu
cd "$(dirname "$0")/.." || exit 1
rsync -a monitor:/data1/logs/figures-ecdsa.jsonl figures-ecdsa.jsonl
mkdir -p figures-ecdsa
python3 scripts/fx-plot-figures.py figures-ecdsa.jsonl figures-ecdsa/
