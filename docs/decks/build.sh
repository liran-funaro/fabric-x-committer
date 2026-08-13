#!/usr/bin/env bash
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#

# Renders the Marp decks in this directory.
#
# HTML export is required and needs only Node.js.
# PDF export additionally needs a Chromium-family browser; when none is found the
# PDF step is skipped with guidance rather than failing the build, since the HTML
# deck is self-contained and can be printed to PDF from any browser.
#
# Usage:
#   ./build.sh                 # render every *.md in this directory
#   ./build.sh <file.md> ...   # render specific decks

set -euo pipefail

DECK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="${DECK_DIR}/out"
MARP_VERSION="4.5.0"

if ! command -v node >/dev/null 2>&1; then
  echo "error: node is required to render the deck (see https://nodejs.org)" >&2
  exit 1
fi

# Locate a Chromium-family browser for PDF export. A browser found here is still
# only a candidate — it may fail to launch on missing shared libraries, which is
# why the PDF step below tolerates failure.
find_browser() {
  if [[ -n "${CHROME_PATH:-}" && -x "${CHROME_PATH}" ]]; then
    echo "${CHROME_PATH}"
    return
  fi
  local candidate
  for candidate in chromium-headless chromium chromium-browser google-chrome chrome; do
    if command -v "${candidate}" >/dev/null 2>&1; then
      command -v "${candidate}"
      return
    fi
  done
  # Browser fetched by `npx puppeteer browsers install chrome`.
  for candidate in "${HOME}"/.cache/puppeteer/chrome/*/chrome-linux64/chrome; do
    if [[ -x "${candidate}" ]]; then
      echo "${candidate}"
      return
    fi
  done
}

LOCAL_MARP_DIR="${DECK_DIR}/.marp"
LOCAL_MARP_BIN="${LOCAL_MARP_DIR}/node_modules/.bin/marp"

# Resolve the marp binary once. A cached local install is preferred over `npx`,
# which re-resolves the package on every invocation and can stall for minutes.
resolve_marp() {
  if [[ -n "${MARP_BIN:-}" && -x "${MARP_BIN}" ]]; then
    echo "${MARP_BIN}"
    return
  fi
  if [[ -x "${LOCAL_MARP_BIN}" ]]; then
    echo "${LOCAL_MARP_BIN}"
    return
  fi
  if command -v marp >/dev/null 2>&1; then
    command -v marp
    return
  fi
  echo "installing @marp-team/marp-cli@${MARP_VERSION} into ${LOCAL_MARP_DIR} ..." >&2
  mkdir -p "${LOCAL_MARP_DIR}"
  npm install --silent --no-fund --no-audit \
    --prefix "${LOCAL_MARP_DIR}" "@marp-team/marp-cli@${MARP_VERSION}" >&2
  echo "${LOCAL_MARP_BIN}"
}

MARP="$(resolve_marp)"

marp() {
  # --html permits the inline SVG diagrams and layout markup the decks rely on.
  "${MARP}" --html --allow-local-files "$@"
}

decks=()
if [[ $# -gt 0 ]]; then
  decks=("$@")
else
  while IFS= read -r deck; do
    decks+=("${deck}")
  done < <(find "${DECK_DIR}" -maxdepth 1 -name '*.md' ! -name 'README.md' | sort)
fi

if [[ ${#decks[@]} -eq 0 ]]; then
  echo "no decks found in ${DECK_DIR}" >&2
  exit 1
fi

mkdir -p "${OUT_DIR}"
browser="$(find_browser || true)"

for deck in "${decks[@]}"; do
  name="$(basename "${deck}" .md)"

  echo "==> ${name}.html"
  marp "${deck}" -o "${OUT_DIR}/${name}.html"

  if [[ -z "${browser}" ]]; then
    continue
  fi

  echo "==> ${name}.pdf"
  if ! CHROME_PATH="${browser}" marp --pdf "${deck}" -o "${OUT_DIR}/${name}.pdf"; then
    echo "note: PDF export failed using ${browser}; the HTML deck is unaffected." >&2
    browser=""
  fi
done

echo
echo "HTML written to ${OUT_DIR}/"
if [[ -z "${browser}" ]]; then
  cat >&2 <<'EOF'

PDF was not produced — no working Chromium-family browser was found.
Either install one, for example:

    sudo dnf install -y chromium-headless    # RHEL/Fedora (EPEL)
    sudo apt-get install -y chromium         # Debian/Ubuntu

then re-run this script; or open the HTML deck in a browser and print to PDF.
EOF
fi
