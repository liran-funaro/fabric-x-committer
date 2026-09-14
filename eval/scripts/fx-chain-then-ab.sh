#!/usr/bin/env bash
# Launch the insert_ns A/B once the 14x queue exits. Detached, so it survives the session that
# armed it -- the batches ahead of it run for hours and every previous chain lost its launcher
# when a session ended.
#
# Single-flight: two sessions arming this independently is how a ten-batch queue was killed once
# already (see fx-plan-14x-lf.sh's header). mkdir is the lock because it is atomic on the one
# filesystem both would use.
set -u
cd /data1/logs || exit 1
LOCK=/data1/logs/.ab-launcher.lock
mkdir "$LOCK" 2>/dev/null || { echo "### $(date +%H:%M:%S) another launcher holds $LOCK; exiting"; exit 0; }
trap 'rmdir "$LOCK" 2>/dev/null' EXIT

echo "### $(date +%H:%M:%S) waiting for fx-plan-14x-lf.sh to exit"
while pgrep -f "[f]x-plan-14x-lf.sh" >/dev/null; do sleep 60; done
echo "### $(date +%H:%M:%S) 14x gone; waiting for the last batch to quiesce"
while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 60; done

# Refuse to run if 14x did not actually get through its queue. A chain killed at batch 3 leaves the
# baseline half-measured, and the A/B would then be compared against rungs that were never taken.
DONE=$(grep -c " done$" plan-14x-lf.log 2>/dev/null || echo 0)
FAILED=$(grep -c "FAILED rc=" plan-14x-lf.log 2>/dev/null || echo 0)
echo "### $(date +%H:%M:%S) 14x finished: $DONE batches done, $FAILED failed"
if [ "$DONE" -lt 9 ]; then
  echo "### $(date +%H:%M:%S) only $DONE of 9 batches completed -- NOT starting the A/B."
  echo "### The A/B overwrites insert_ns permanently, so it must not run on a half-measured baseline."
  echo "### Start it by hand after the gap is understood: setsid ./fx-plan-14z-ab.sh > plan-14z-ab.log 2>&1 &"
  exit 1
fi
echo "### $(date +%H:%M:%S) starting the insert_ns A/B"
setsid ./fx-plan-14z-ab.sh > plan-14z-ab.log 2>&1 < /dev/null &
echo "### $(date +%H:%M:%S) launched pid $!"
