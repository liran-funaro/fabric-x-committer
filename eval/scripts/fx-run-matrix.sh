#!/usr/bin/env bash
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
# Bring an arm up cleanly, prove it is the arm asked for, then run experiments on it.
#
# This is the outer loop of every measurement in eval/evaluation.tex: fx-bringup.sh puts the cluster in
# a measurable state, this script proves it did, and fx-figures.py measures. Run it detached, on the
# control node, and read the log rather than waiting on it -- a matrix runs for hours.
#
#   INV=inventory/cluster.yaml ONLY=9a-,9b- OUT=/data1/logs/figures.jsonl ./fx-run-matrix.sh
#
#   INV     which arm (default: the committer-only one)
#   ONLY    comma-separated experiment ids or id prefixes; empty runs the whole matrix
#   OUT     results file; one file per scheme or arm, since ids repeat across them
#   SCHEME  the signature scheme to insist on, read back off the running generator
#   HOURS   deadline, checked before each experiment (default 11)
#   REDO    ids to re-measure even though the results file already holds a confirmed hold for them --
#           what to pass after a change that invalidates a point rather than adds one
#   E2E     1 selects the end-to-end experiment list, and with it `setup` per point rather than
#           `configs`: on that arm teardown takes the crypto and each node's genesis block with it,
#           so re-rendering configuration alone brings the orderer back with no identity
#
# The guards are here because each one has cost a run. Two drivers on one cluster is the worst of them:
# both call `make limit-rate`, their plays collide, every rate-set returns rc=2, and the matrix
# "completes" in a minute having measured nothing.
set -u
cd "$(dirname "$0")" || exit 1
LOGS=${LOGS:-/data1/logs}

ONLY=${ONLY:-}
SCHEME=${SCHEME:-ECDSA}
HOURS=${HOURS:-11}
E2E=${E2E:-0}
if [ "$E2E" = "1" ]; then
  MATRIX=e2e
  PLAN=${PLAN:-setup}
  OUT=${OUT:-$LOGS/figures-orderer.jsonl}
  INV=${INV:-/data1/cluster/inventory/cluster-orderer.yaml}
else
  MATRIX=
  PLAN=${PLAN:-configs}
  OUT=${OUT:-$LOGS/figures.jsonl}
  INV=${INV:-/data1/cluster/inventory/cluster.yaml}
fi
TAG=${TAG:-matrix}
say() { echo "### $(date +%H:%M:%S) $*"; }

# One driver only, and no play of anyone else's in flight. Count fx-figures.py processes excluding
# this script's own pid, so the pattern cannot match the shell running it.
BUSY=$(pgrep -f "fx-figures.py" | grep -vc "^$$\$" || true)
if [ "${BUSY:-0}" -gt 0 ] || pgrep -f "[a]nsible-playbook" >/dev/null; then
  say "!! another driver or play is running; refusing to start a second"
  pgrep -af "fx-figures.py|[a]nsible-playbook" | head -3
  exit 1
fi

say "bring up: $INV"
if ! INV=$INV ./fx-bringup.sh > "$LOGS/bringup-$TAG.log" 2>&1; then
  say "!! bring-up failed; last gates:"
  grep -E "^!! |^=== " "$LOGS/bringup-$TAG.log" | tail -12
  exit 1
fi

# Gate on the artifacts, not on the bring-up's exit code: read the arm and the scheme off the config
# the load generator is actually running, found from its own command line.
LGHOST=${LGHOST:-10.241.64.9}
sshq() { ssh -o StrictHostKeyChecking=no "$LGHOST" "$@" 2>/dev/null; }
CFG=$(sshq "pgrep -af '[l]oadgen start' | grep -oE '\-\-config=[^ ]+' | head -1 | cut -d= -f2")
say "generator config in use: ${CFG:-<none>}"
[ -n "$CFG" ] || { say "!! no running load generator"; exit 1; }

MOCK=$(sshq "grep -c 'mock orderer' $CFG" || echo 0)
GOT=$(sshq "awk '/namespace-policies:/{f=1} f && /^[[:space:]]*scheme:/{print \$2; exit}' $CFG")
say "mock-orderer refs: $MOCK   scheme: ${GOT:-<none>}"
[ "$GOT" = "$SCHEME" ] || { say "!! scheme is $GOT, expected $SCHEME"; exit 1; }
# A mock orderer in the generator's config means the committer-only arm, and its absence the
# end-to-end one. Which is correct depends on the inventory, so check they agree rather than assume.
if grep -q "orderer_component_type" "$INV"; then
  [ "${MOCK:-0}" -eq 0 ] || { say "!! end-to-end inventory but the generator has a mock orderer"; exit 1; }
else
  [ "${MOCK:-0}" -gt 0 ] || { say "!! committer-only inventory but no mock orderer in the generator"; exit 1; }
fi

# The rate limiter is what every measurement sets a rate through. Prove it answers before spending
# hours discovering it does not.
say "checking the rate limiter responds"
if ! ANSIBLE_INVENTORY=$INV bash -c \
     'source /data1/cluster/bin/fx-env.sh; cd "$FX_PROJECT"; make limit-rate LIMIT=50000' \
     > "$LOGS/limit-rate-$TAG.log" 2>&1; then
  say "!! limit-rate failed; the matrix cannot set rates"
  grep -E "fatal|ERROR" "$LOGS/limit-rate-$TAG.log" | head -3
  exit 1
fi
say "rate limiter answers"

# REDO is echoed because it is the one input whose absence looks like success: passing FX_REDO in the
# environment instead of REDO here is silently overridden by the assignment below, and the run then
# skips every experiment it was meant to re-measure and still reports MATRIX COMPLETE.
say "matrix: ${ONLY:-<everything>} -> $OUT   redo=${REDO:-<none>}"
# The driver writes its own log. Callers redirect this script to $TAG.log, and when the driver used
# that name too the two truncated each other.
FX_ONLY=$ONLY FX_REDO=${REDO:-} FX_DEADLINE_HOURS=$HOURS FX_OUT=$OUT FX_INVENTORY=$INV \
FX_MATRIX=$MATRIX FX_DEPLOY_PLAN=$PLAN \
  python3 ./fx-figures.py > "$LOGS/$TAG-driver.log" 2>&1
grep -E "hold limit|no rate met|deadline|run done" "$LOGS/$TAG-driver.log" | tail -20
say "MATRIX COMPLETE: $TAG"
