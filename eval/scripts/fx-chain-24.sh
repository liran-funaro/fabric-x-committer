#!/usr/bin/env bash
# Supervisor: run the A/B, then the remaining batches, without needing anyone watching.
#
# It exists because the two scripts must not overlap -- fx-run-matrix.sh refuses a second driver, so a
# chain started early fails its first batch and then carries on failing every batch after it -- and
# because the A/B leaves the BASELINE binary staged on its way out, which is the precondition
# fx-plan-24c-rest.sh checks on entry. Running them from one supervisor keeps that ordering even if the
# session driving it goes away.
set -u
say() { echo "### $(date -u +%H:%M:%SZ) [supervisor] $*"; }

# Wait for whatever is in flight now (rep3) before starting the A/B, using the same three-way check the
# chains use: the driver, the matrix wrapper, and any play.
busy() { pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "[f]x-run-matrix" >/dev/null \
         || pgrep -f "/[a]nsible-playbook " >/dev/null; }

if pgrep -f "[f]x-plan-24b" >/dev/null; then
  say "fx-plan-24b-ab.sh is already queued; waiting for it rather than starting a second"
else
  while busy; do sleep 30; done
  say "starting the A/B"
  /data1/scripts/fx-plan-24b-ab.sh >> /data1/logs/plan-24b.log 2>&1
fi

# The A/B may be the one started outside this supervisor, so wait on the process rather than on our own
# child. Then gate on the artifact it promised to leave behind.
while pgrep -f "[f]x-plan-24b" >/dev/null; do sleep 30; done
while busy; do sleep 30; done

# If the A/B exited early after its swap -- a failed gate, a killed run -- the rewrite is still staged and
# batches 4-9 would measure the path the rewrite removes, which makes them meaningless. Recover rather
# than stall: /data1/bin-stage.baseline is a byte copy taken before the swap, so restoring from it is
# deterministic rather than a guess, and it is re-verified afterwards. Refuse only if that fails too,
# because at that point the staged binary's provenance is genuinely unknown.
EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
if [ "$EA" != "0" ]; then
  say "!! the A/B left EXCEPT ALL=$EA staged; restoring the baseline from /data1/bin-stage.baseline"
  if [ -f /data1/bin-stage.baseline/committer ]; then
    install -m 0750 /data1/bin-stage.baseline/committer /data1/bin-stage.baseline/loadgen /data1/bin-stage/
    EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
  fi
fi
if [ "$EA" != "0" ]; then
  say "!! could not put the baseline back (EXCEPT ALL=$EA). NOT starting batches 4-9: they measure the"
  say "!! failure path the rewrite removes and would be meaningless against the rewrite."
  say "!! the workstation holds both variants; re-stage from there."
  exit 1
fi
say "baseline confirmed staged (EXCEPT ALL=0); starting batches 2k and 4-9"
/data1/scripts/fx-plan-24c-rest.sh >> /data1/logs/plan-24c.log 2>&1
say "ALL QUEUED WORK COMPLETE"
