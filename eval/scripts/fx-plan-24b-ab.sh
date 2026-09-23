#!/usr/bin/env bash
# The insert_ns A/B, plus the restore that makes it reversible.
#
# THE ORDERING ARGUMENT RECORDED ELSEWHERE ASSUMES THIS SWAP IS ONE-WAY. It is not, in this setup.
# eval-todo.md, cluster-optimization-log.md section 13 and fx-figures.py all reason from "CREATE OR
# REPLACE is not a live upgrade path, so after the swap the baseline batches cannot be taken at all".
# That is true of a LIVE cluster and false of this harness: fx-bringup.sh wipes every database data
# directory and the namespaces are recreated from scratch on the next bring-up, so the function
# definition that lands is whichever binary sits in /data1/bin-stage. Both variants are staged:
#
#   /data1/bin-stage          the baseline, EXCEPT ALL = 0   (EXCEPTION WHEN unique_violation)
#   /data1/bin-stage-rewrite  the rewrite,  EXCEPT ALL = 2   (ON CONFLICT ... EXCEPT ALL)
#
# So the A/B can run whenever, and the baseline batches can run after it. This script swaps in the
# rewrite, runs both sides of the A/B, and swaps the baseline back so a later chain is unaffected even
# if it is started by someone who has not read this.
set -u
cd /data1/scripts || exit 1
say() { echo "### $(date -u +%H:%M:%SZ) $*"; }
# Also waits on fx-run-matrix.sh, not just the driver and the playbook. Between a bring-up
# finishing and fx-figures.py starting, those two are both absent for a few seconds -- long
# enough for a waiting chain to swap the staged binary out from under a batch that is about to
# measure with it.
idle() { while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "[f]x-run-matrix" >/dev/null \
                || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done; }

C=/data1/cluster/inventory/cluster.yaml

# --- ARM 1, on the BASELINE binary: the conflict-free hold, TODAY. --------------------------------
# This arm exists because of a methodological trap, not for completeness. The rewrite's regression test
# is "does the conflict-free path still do ~500,000", and 9c-ds0's recorded holds are 518,000 / 529,818
# / 541,273 / 544,364 -- a 5% spread across days, on a cluster whose baseline is recorded as moving
# 10-15% between days. A 5% regression from materialising the RETURNING set would therefore be
# indistinguishable from drift if the comparison crossed days, and this cluster was rebuilt from bare
# metal this morning. REDO because it already has met rows and would otherwise be skipped.
#
# The conflict SIDE gets no same-day baseline arm, deliberately: that baseline is "no offered rate meets
# the bound at the 120-way split", established over 70 rows plus ladderlow's five uncensored sub-capacity
# rungs, and the effect being looked for is 20,300 against a possible 100,000+. Day drift cannot reach
# across that. Spending a bring-up to re-confirm it would cost an hour to tighten a comparison that is
# already an order of magnitude clear.
idle
say "ARM 1 (baseline binary): conflict-free hold, same day"
EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
if [ "$EA" != "0" ]; then say "!! expected the BASELINE staged (EXCEPT ALL=0), found $EA; aborting"; exit 1; fi
if TAG=dsbase0 ONLY='9c-ds0$' REDO=9c-ds0 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=4 \
   INV=$C FX_DRAIN_RATE=20000 ./fx-run-matrix.sh > /data1/logs/dsbase0.log 2>&1
then say "dsbase0 done"; else say "dsbase0 FAILED rc=$? -- see /data1/logs/dsbase0.log"; fi

idle
say "staging the REWRITE binaries"
mkdir -p /data1/bin-stage.baseline
install -m 0750 /data1/bin-stage/committer /data1/bin-stage/loadgen /data1/bin-stage.baseline/
install -m 0750 /data1/bin-stage-rewrite/committer /data1/bin-stage-rewrite/loadgen /data1/bin-stage/

# Gate on the artifact, not on cp's exit code. EXCEPT ALL is the only present/absent test:
# unique_violation reads 2 against 1 (init_database_tmpl.sql has its own handler) and ON CONFLICT
# reads 3 in both, so either of those alone would pass a wrong binary.
EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
if [ "$EA" != "2" ]; then say "!! staged binary is NOT the rewrite (EXCEPT ALL=$EA); aborting"; exit 1; fi
say "staged binary verified as the rewrite (EXCEPT ALL=2)"

run() { idle; say "$1"
  if TAG=$1 ONLY=$2 OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=4 INV=$3 \
     FX_DRAIN_RATE=${4:-20000} ./fx-run-matrix.sh > "/data1/logs/$1.log" 2>&1
  then say "$1 done"
  else say "$1 FAILED rc=$? -- see /data1/logs/$1.log"
  fi; }

# The conflict side first: 5% double spends at the 120-way pre-split, the configuration that currently
# meets the bound at NO offered rate. A ladder (20k/50k/100k/200k/400k) rather than a search, because if
# the rewrite works the capacity is unknown -- the old code retires 20,300 and the conflict-free ceiling
# on this arm is 518,000, so the rungs have to span both.
#
# Prediction on record before this runs (log section 10): db_insert falls from ~1.7 s to tens of ms and
# the bound is met far above 20,300. Falsifier: db_insert still in the hundreds of ms means the
# full-batch lookup was not the cost and the write-path hypothesis takes over.
say "ARM 2 (rewrite binary): 5% double spends at the 120-way split"
run dsfix5 9c-ds5-onconflict $C 20000

# Gate on what the DATABASE holds, not on what was staged. The binary check proves which SQL text was
# shipped; this proves which function the namespace actually got, and they can differ -- CREATE OR
# REPLACE does not touch a namespace that already exists, so a table surviving a wipe keeps the old
# function while the new binary sits on disk. Run while the cluster is still up: teardown happens at the
# NEXT bring-up, so this window is the only chance.
YSQL=$(ssh -o StrictHostKeyChecking=no 10.241.64.10 'ls /data1/fabric-x/yugabyte/*/bin/ysqlsh 2>/dev/null | head -1' 2>/dev/null)
if [ -n "$YSQL" ]; then
  DEF=$(ssh -o StrictHostKeyChecking=no 10.241.64.10 \
        "$YSQL -h 10.241.64.10 -p 5320 -U yugabyte -d yugabyte -At -c \
        \"select (pg_get_functiondef(p.oid) like '%EXCEPT ALL%')::text from pg_proc p where p.proname = 'insert_ns_0'\"" 2>/dev/null)
  say "live insert_ns_0 is the rewrite: ${DEF:-<query failed or namespace absent>}"
  # insert_ns_0 SPECIFICALLY, not `like 'insert_ns%'`. The four system namespaces -- _checkpoint,
  # _config, _meta, _snapshot -- are created at bring-up and would already carry the rewrite, so a
  # count over the pattern reads healthy even when the namespace under test carries the old function,
  # which is the only one whose rows are the measurement.
  [ "${DEF:-f}" != "true" ] && say "!! WARNING: insert_ns_0 is NOT the rewrite -- dsfix5's rows do not measure it"
else
  say "!! could not locate ysqlsh; deployed-function check SKIPPED"
fi

# The regression side, and the only place the rewrite can cost something: the common path now
# materialises a RETURNING set and compares cardinalities where it returned '{}' after a bare INSERT, at
# ~3,400 calls a second per validator-committer. Read db_commit as well as db_insert: db_insert wraps
# insertStates only, so a cost landing in the surrounding transaction shows in one and not the other.
# Falsifier for "this is a fix rather than a trade": anything below ~500,000 here.
say "ARM 3 (rewrite binary): the conflict-free hold, to pair with ARM 1"
run dsfix0 9c-ds0-onconflict $C 20000

# Swap the baseline back, verified, so the remaining characterisation batches are unaffected.
idle
say "restoring the BASELINE binaries"
install -m 0750 /data1/bin-stage.baseline/committer /data1/bin-stage.baseline/loadgen /data1/bin-stage/
EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
if [ "$EA" != "0" ]; then say "!! restore FAILED: staged binary still has EXCEPT ALL=$EA"; exit 1; fi
say "baseline restored and verified (EXCEPT ALL=0)"
say "A/B DONE"
