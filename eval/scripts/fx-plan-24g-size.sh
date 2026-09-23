#!/usr/bin/env bash
# The end-to-end size sweep: the only figure in the paper still missing data.
#
# figures-e2e/size-throughput.pdf needs six points. The committed results hold three stale confirmed
# holds (300 B at 408,000, 512 B at 414,364, 2 KiB at 155,936), no hold at all for 1 KiB or 3 KiB, and a
# FAILED hold for 4 KiB that recorded finished=0. Better values for four of them were measured on
# 2026-09-22 and exist only in prose -- their rows died with the fleet, so they cannot be plotted.
# Hence REDO on all six: without it the driver skips the three that have holds.
#
# 4 KiB is the one to watch. Its last hold reported finished=0 with in-flight growing 25,902/s at a rate
# a probe had just met at 493 ms. If that repeats, the point has a probe and no hold, and the figure has
# to say so rather than interpolate across it.
#
# Needs all twenty orderer machines. assembler3 was down for most of 2026-09-23 and missed the fleet
# preparation, so its data disks were formatted separately before this could run. Three assemblers is a
# different ordering tier and would not be comparable with the rows this replaces.
set -u
cd /data1/scripts || exit 1
say() { echo "### $(date -u +%H:%M:%SZ) $*"; }
idle() { while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "[f]x-run-matrix" >/dev/null \
                || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done; }

O=/data1/cluster/inventory/cluster-orderer.yaml

idle
# Gate on the machines, not on the plan: a missing assembler silently changes the configuration, and
# this sweep exists to replace rows that a partial fleet already invalidated once.
for h in $(seq 23 42); do
  if ! timeout 5 ping -c1 -W2 10.241.64.$h >/dev/null 2>&1; then
    say "!! 10.241.64.$h is unreachable; the sweep needs all twenty orderer machines. Aborting."
    exit 1
  fi
done
say "all twenty orderer machines answer"

say "end-to-end size sweep, all six points"
if TAG=e2esize E2E=1 ONLY=e2e-size \
   REDO=e2e-size300,e2e-size512,e2e-size1024,e2e-size2048,e2e-size3072,e2e-size4096 \
   OUT=/data1/logs/figures-orderer.jsonl SCHEME=ECDSA HOURS=10 INV=$O \
   ./fx-run-matrix.sh > /data1/logs/e2esize.log 2>&1
then say "e2esize done"; else say "e2esize FAILED rc=$? -- see /data1/logs/e2esize.log"; fi
say "SIZE SWEEP DONE"
