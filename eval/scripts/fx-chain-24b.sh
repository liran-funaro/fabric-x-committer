#!/usr/bin/env bash
# Supervisor v2: A/B -> the rewrite's sub-capacity ladder -> the baseline characterisation batches.
#
# v1 went straight from the A/B to fx-plan-24c-rest.sh. The A/B's first conflict rung changed what is
# worth running next: with the rewrite live, 20,000 offered retires 12,727 at a 2.802 s insert, so its
# whole ladder is over capacity and censored, and the comparison that decides whether the rewrite did
# anything is ladderlow's four SUB-capacity rates. fx-plan-24d-lowfix.sh runs exactly those, on the
# rewrite, and restores the baseline afterwards -- which is the precondition 24c checks on entry.
set -u
say() { echo "### $(date -u +%H:%M:%SZ) [supervisor2] $*"; }
busy() { pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "[f]x-run-matrix" >/dev/null \
         || pgrep -f "/[a]nsible-playbook " >/dev/null; }

say "waiting for the A/B to finish"
while pgrep -f "[f]x-plan-24b" >/dev/null; do sleep 30; done
while busy; do sleep 30; done
say "A/B finished"

say "starting the rewrite's sub-capacity ladder"
/data1/scripts/fx-plan-24d-lowfix.sh >> /data1/logs/plan-24d.log 2>&1
while busy; do sleep 30; done

# Deterministic recovery, then refuse only if it fails: 24c's batches measure the failure path the
# rewrite is meant to remove and are meaningless against the rewrite.
EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
if [ "$EA" != "0" ] && [ -f /data1/bin-stage.baseline/committer ]; then
  say "!! EXCEPT ALL=$EA staged; restoring the baseline from /data1/bin-stage.baseline"
  install -m 0750 /data1/bin-stage.baseline/committer /data1/bin-stage.baseline/loadgen /data1/bin-stage/
  EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
fi
if [ "$EA" != "0" ]; then
  say "!! could not put the baseline back (EXCEPT ALL=$EA). NOT starting batches 4-9."
  exit 1
fi
say "baseline confirmed staged (EXCEPT ALL=0); starting batches 2k and 4-9"
/data1/scripts/fx-plan-24c-rest.sh >> /data1/logs/plan-24c.log 2>&1
say "ALL QUEUED WORK COMPLETE"
