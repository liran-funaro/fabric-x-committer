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
# Markdown that survives into LaTeX prints literally and exits 0, so the build cannot catch it and the
# reader finds it in the PDF. `**bold**` set four asterisks into a sentence this week. Checked before
# pdflatex runs, because this is the one class of error the compiler is happy about: `**` is legal TeX
# (two \emph-less asterisks), as are the `_ _` and backticks that come from the same habit. Three
# sessions write this file from Markdown notes, so the habit is structural rather than anyone's slip.
bad=$(grep -nE '\*\*|``[^`]|(^|[^\\])_[A-Za-z][A-Za-z ]*_([^A-Za-z]|$)' evaluation.tex || true)
[ -z "$bad" ] || { echo "Markdown leaked into the LaTeX:"; echo "$bad"; exit 1; }
for pass in 1 2; do
  pdflatex -halt-on-error -interaction=nonstopmode evaluation.tex >/tmp/evaluation-pass$pass.log ||
    { echo "pass $pass failed:"; grep -A3 "^!" /tmp/evaluation-pass$pass.log | head -20; exit 1; }
done
# An unresolved reference still exits 0, so it has to be looked for rather than waited for.
grep -c "undefined references\|Reference .* undefined" /tmp/evaluation-pass2.log >/dev/null &&
  { echo "unresolved references:"; grep "Reference .* undefined" /tmp/evaluation-pass2.log; exit 1; }
rm -f evaluation.aux evaluation.log evaluation.out
echo "evaluation.pdf: $(pdfinfo evaluation.pdf | awk '/^Pages/{print $2}') pages"
