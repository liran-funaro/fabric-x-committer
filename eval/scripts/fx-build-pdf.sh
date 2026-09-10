#!/usr/bin/env bash
# Build eval/evaluation.pdf.
#
# Twice, because the document cross-references its own figures and tables: the first pass writes the
# numbers into evaluation.aux and the second resolves the \ref that read them. A single pass leaves
# "??" wherever a label is new or has moved.
#
# Figures are read from eval/figures*/ as PDFs; regenerate them with fx-plot-figures.py (committer),
# fx-plot-e2e.sh (end to end) or fx-plot-ecdsa.sh first if the measurements changed.
set -eu
cd "$(dirname "$0")/.." || exit 1
for pass in 1 2; do
  pdflatex -halt-on-error -interaction=nonstopmode evaluation.tex >/tmp/evaluation-pass$pass.log ||
    { echo "pass $pass failed:"; grep -A3 "^!" /tmp/evaluation-pass$pass.log | head -20; exit 1; }
done
# An unresolved reference still exits 0, so it has to be looked for rather than waited for.
grep -c "undefined references\|Reference .* undefined" /tmp/evaluation-pass2.log >/dev/null &&
  { echo "unresolved references:"; grep "Reference .* undefined" /tmp/evaluation-pass2.log; exit 1; }
rm -f evaluation.aux evaluation.log evaluation.out
echo "evaluation.pdf: $(pdfinfo evaluation.pdf | awk '/^Pages/{print $2}') pages"
