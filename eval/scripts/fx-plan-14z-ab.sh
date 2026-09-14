#!/usr/bin/env bash
# Session -14's queue, ordered by what the document still needs rather than by what is interesting.
#
# Named -lf rather than -14q because two sessions independently wrote different fx-plan-14p.sh files
# today and the second overwrote the first, which killed a ten-batch queue and left the cluster running
# one batch. A number is not a name when more than one session is picking numbers.
set -u
cd /data1/logs || exit 1
say() { echo "### $(date +%H:%M:%S) $*"; }
idle() { while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done; }
C=/data1/cluster/inventory/cluster.yaml
# Automatic tablet splitting pinned OFF. It has to be an INVENTORY, not an experiment var: a
# per-experiment redeploy tears down `fabric_x_committers:load_generators` and nothing else, so the
# yb-master is never restarted and a gflag set in exp-vars.yaml is a silent no-op. Only a batch bring-up
# restarts the database, and that reads $INV. Verify after bring-up rather than trusting the file:
#   curl -sk https://10.241.64.10:5310/api/v1/varz | python3 -c ... | grep automatic_tablet
NS=/data1/cluster/inventory/cluster-nosplitting.yaml
# 4th arg: drain rate. 5th: search seed -- and a seed can only probe down to seed x 0.85^6, so a seed
# carried over from a conflict-free shape cannot reach a conflict knee and the batch reports "no rate met"
# indistinguishably from there being no usable rate. Fixed-rate ladders take no seed at all, which is one
# of the reasons most of this queue is now ladders.
# SKIP_DEPLOY measures whatever is already deployed, refusing any experiment whose rendered shape does not
# match. For the ageing test that is not an optimisation, it IS the experiment -- and it closes a trap,
# since a missed rung inside a normal ladder triggers a redeploy, which would hand the measurement a fresh
# table and read as "still 14 ms, durable".
runskip() { idle; say "$1 (no deploy -- measuring the table as it stands)"
  if TAG=$1 ONLY=$2 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=2 INV=$3 \
     SKIP_DEPLOY=1 FX_DRAIN_RATE=${4:-20000} ./fx-run-matrix.sh > "/data1/logs/$1.log" 2>&1
  then say "$1 done"
  else say "$1 FAILED rc=$? -- see /data1/logs/$1.log"
  fi; }

run() { idle; say "$1 (drain ${4:-20000}, seed ${5:-480000})"
  if TAG=$1 ONLY=$2 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=4 INV=$3 \
     FX_DRAIN_RATE=${4:-20000} FX_SEED=${5:-480000} ./fx-run-matrix.sh > "/data1/logs/$1.log" 2>&1
  then say "$1 done"
  else say "$1 FAILED rc=$? -- see /data1/logs/$1.log"
  fi; }

# 1. The nosplit batch is running already, under the chain this one replaced, so it is not repeated.

# ---------------------------------------------------------------------------------------------
# BATCH 0 (which runs LAST) -- the insert_ns rewrite.
#
# Sanctioned by the user directly: "I agree to the SQL work. Do it." and again "I agree to the SQL
# work." Applied in committer d44ef3a4; the hold in fbb160b9 is lifted. It runs after the
# characterisation batches, not before them, because CREATE OR REPLACE is not a live upgrade path:
# once a bring-up creates the namespace with the new function every later bring-up keeps it, so
# ladderlow, ladder8tab, tabhold and the share sweep -- which all measure the failure path this
# rewrite removes -- become impossible the moment it lands. Running it last costs nothing and gives
# it a measured baseline instead of an assumed one. No message from the user named an order; that
# argument is the session's, and the eval TODO records it as such.
#
# ON CONFLICT (key) DO NOTHING ... RETURNING key replaces the EXCEPTION WHEN unique_violation
# handler, so detecting a collision no longer scans the whole batch with key = ANY(_keys).
# ---------------------------------------------------------------------------------------------

STAGE=/data1/bin-stage
# The deploy mechanism is a file swap, not a rebuild: fx-bringup.sh installs $STAGE/committer into
# $FX_PROJECT/out/control-node/bin/Linux/x86_64/ on every bring-up, and the SQL is go:embed'ed. Both
# variants are staged under non-live names so this is one mv, and reversible with the other one.
#   committer.exception  -- WHEN unique_violation handler, what every rung recorded today measured
#   committer.onconflict -- ON CONFLICT DO NOTHING, built from d44ef3a4 for linux/amd64
sql_variant() {  # $1 = exception|onconflict
  [ -f "$STAGE/committer.$1" ] || { say "!! $STAGE/committer.$1 missing; not swapping"; return 1; }
  install -m 0750 "$STAGE/committer.$1" "$STAGE/committer" || return 1
  say "staged committer := $1 ($(strings "$STAGE/committer" | grep -c 'ON CONFLICT (key) DO NOTHING') ON CONFLICT)"
}

# Pre-check 2 from the eval TODO: the function actually live in the running database. A null result
# is otherwise indistinguishable from a failed deploy, and this is the exact failure mode -- a
# namespace created before the swap keeps its old function no matter what binary is on disk.
# ysql is on 5320 with TLS, and a tserver-local connection authenticates on trust, so no credential
# is read or passed here.
YB=/data1/fabric-x/yugabyte/yugabyte-2025.2.1.0/bin/ysqlsh
# Three tservers, not one: a tserver-local ysql on 5320 answered, then refused, then answered again
# inside ten minutes while the process stayed up, so a single host makes a transient look like a
# failed deploy -- which is the one thing this gate exists to distinguish.
#
# The SQL goes over stdin rather than in -c. Passing it as an argument means quoting it through two
# shells, and the first version of this did that wrong: \$LIVE_SQL expanded on the REMOTE host, where
# it is unset, so ysqlsh got an empty query and every tserver read as unreachable. That failure was
# silent in the safe direction, which is exactly why it needed a check that asserts both answers.
LIVE_SQL="select proname || '=' || case when position('ON CONFLICT' in pg_get_functiondef(oid))>0 then 'onconflict' else 'handler' end from pg_proc where proname = 'insert_ns_0';"
live_function() {
  local ts out
  for ts in 10.241.64.13 10.241.64.14 10.241.64.15; do
    out=$(printf '%s\n' "$LIVE_SQL" |
          ssh -o ConnectTimeout=10 "$ts" "$YB -h $ts -p 5320 -U yugabyte -d yugabyte -tA" 2>/dev/null |
          head -1)
    case "$out" in insert_ns_0=*) echo "$out"; return 0;; esac
  done
  echo "unreachable-on-all-three-tservers"; return 1
}
gate_live() {  # $1 = expected: onconflict|handler
  local got; got=$(live_function)
  say "live insert_ns_0: $got (expected $1)"
  case "$got" in *"=$1") return 0;; esac
  say "!! LIVE FUNCTION IS NOT $1 -- the numbers below measure the other variant. Read this before quoting them."
  return 1
}

# Where the signal is: the 120-way split. The fan-out costs 1.7 s there against 13.6 ms at 12
# tablets, so a null at 12 tablets would prove nothing about the rewrite. $C pre-splits 120.
say "=== batch 0: insert_ns A/B, on top of a completed 14x queue ==="
gate_live handler || say "(the pre-swap baseline was not the handler; note it and continue)"

sql_variant onconflict || exit 1

# A ladder rather than a search: if the rewrite works the capacity is unknown, and the old code
# retires 20,300 while the conflict-free ceiling on this arm is 518,000, so a seed cannot bracket it.
run oncf   9c-ds5-onconflict $C 20000
gate_live onconflict

# The regression side. The common path now materialises a RETURNING set and diffs cardinalities
# where it returned '{}' after a bare INSERT, at ~3,400 calls a second per VC. Read db_commit as
# well as db_insert: db_insert wraps insertStates only, so a cost landing in the surrounding
# transaction shows in one and not the other.
run oncf0  9c-ds0-onconflict $C 20000
gate_live onconflict

say "=== batch 0 done. Staged committer stays at onconflict -- that is the sanctioned end state."
say "To restore the measured baseline for a re-run: install -m 0750 $STAGE/committer.exception $STAGE/committer"
