#!/usr/bin/env bash
# The rewrite at ladderlow's own rates: the only like-for-like comparison the A/B can support.
#
# 9c-ds5-onconflict was written assuming the rewrite works, so its ladder starts at 20,000. With
# capacity measured at ~12,700 every rung is over capacity and censored at 60 s, and a censored rung
# cannot be compared with ladderlow's uncensored sub-capacity readings, which are the baseline:
#
#   offered  2,500 -> insert 1.707 s   |  10,000 -> 2.220 s
#   offered  5,000 -> insert 1.969 s   |  15,000 -> 2.540 s
#
# Same four rates, same shape, same 120-way layout, one code change apart. If the insert is still
# 1.7-2.5 s at 2,500 offered, where nothing queues anywhere, then ON CONFLICT is not what costs the
# seconds -- and the first over-capacity rung already points that way at 2.802 s.
set -u
cd /data1/scripts || exit 1
say() { echo "### $(date -u +%H:%M:%SZ) $*"; }
idle() { while pgrep -f "[f]x-figures.py" >/dev/null || pgrep -f "[f]x-run-matrix" >/dev/null \
                || pgrep -f "/[a]nsible-playbook " >/dev/null; do sleep 30; done; }

C=/data1/cluster/inventory/cluster.yaml

idle
say "staging the REWRITE binaries for the sub-capacity ladder"
mkdir -p /data1/bin-stage.baseline
[ -f /data1/bin-stage.baseline/committer ] || install -m 0750 /data1/bin-stage/committer /data1/bin-stage/loadgen /data1/bin-stage.baseline/
install -m 0750 /data1/bin-stage-rewrite/committer /data1/bin-stage-rewrite/loadgen /data1/bin-stage/
EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
if [ "$EA" != "2" ]; then say "!! staged binary is NOT the rewrite (EXCEPT ALL=$EA); aborting"; exit 1; fi
say "staged binary verified as the rewrite (EXCEPT ALL=2)"

# FX_DRAIN_RATE is only the fallback -- drain() parks at a tenth of what is actually being retired --
# but set it low anyway, because these rates are near a capacity of ~12,700 and the fallback fires
# whenever the sample returns nothing.
if TAG=dsfixlow ONLY='9c-ds5-onconflict-low$' OUT=/data1/logs/figures-ecdsa.jsonl SCHEME=ECDSA HOURS=4 \
   INV=$C FX_DRAIN_RATE=1000 ./fx-run-matrix.sh > /data1/logs/dsfixlow.log 2>&1
then say "dsfixlow done"; else say "dsfixlow FAILED rc=$? -- see /data1/logs/dsfixlow.log"; fi

# Gate on insert_ns_0 specifically. The four system namespaces are created at bring-up and carry
# whichever variant shipped, so a count over `insert_ns%` reads healthy even when the namespace under
# test does not -- and insert_ns_0 is the only one whose rows are the measurement.
H=10.241.64.13
Y=$(ssh -o StrictHostKeyChecking=no $H 'ls /data1/fabric-x/yugabyte/*/bin/ysqlsh 2>/dev/null | head -1' 2>/dev/null)
if [ -n "$Y" ]; then
  DEF=$(ssh -o StrictHostKeyChecking=no $H "$Y -h $H -p 5320 -U yugabyte -d yugabyte -At -c \
        \"select (pg_get_functiondef(p.oid) like '%EXCEPT ALL%')::text from pg_proc p where p.proname = 'insert_ns_0'\"" 2>/dev/null)
  say "live insert_ns_0 is the rewrite: ${DEF:-<query failed or namespace absent>}"
fi

idle
say "restoring the BASELINE binaries"
install -m 0750 /data1/bin-stage.baseline/committer /data1/bin-stage.baseline/loadgen /data1/bin-stage/
EA=$(strings /data1/bin-stage/committer | grep -c "EXCEPT ALL")
if [ "$EA" != "0" ]; then say "!! restore FAILED: staged binary still has EXCEPT ALL=$EA"; exit 1; fi
say "baseline restored and verified (EXCEPT ALL=0)"
say "SUB-CAPACITY LADDER DONE"
