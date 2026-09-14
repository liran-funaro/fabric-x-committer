#!/usr/bin/env bash
# Record how the state table's tablet LEADERS are spread over the tablet servers, every 60 s.
#
# Why: 250,000 tps at twelve tablets read MET on one deployment and MISSED on the next, with the insert
# at 17.0 ms and 262 ms. Already ruled out -- an undrained pipeline (the table was wiped, SST 33.4 GB ->
# 0.5 GB, and mvcc reset), table size (the SMALLER table is the one that failed) and tablet count (12/12
# both times). Leader placement is the next candidate and was not being recorded: an insert touches every
# tablet, so if a table comes back with several leaders on one tablet server, every batch waits on that
# server and the per-batch cost rises without the layout looking any different.
#
# A separate process rather than a column added to fx-tablet-count.sh, which is mid-loop: bash re-reads a
# script as it runs, so editing that file under it can corrupt what it reads next.
#
# Read-only, master API only. Columns: ts, table, tablets, hosts_with_leaders, max_leaders_on_one_host,
# then the per-host counts. With N tablets over 12 servers, hosts=N and max=1 is perfectly spread.
set -u
M=${FX_MASTER:-https://10.241.64.10:5310}
echo "ts table tablets leader_hosts max_on_one distribution"
while true; do
  curl -sk -m 10 "$M/api/v1/tables" 2>/dev/null > /tmp/ls-tables.json || true
  python3 - "$M" <<'PYEOF'
import collections, json, subprocess, sys, time

M = sys.argv[1]


def note(msg):
    """Say why there is no row, rather than printing nothing.

    A silent sampler is indistinguishable from a dead one. Gaps here are normal -- a bring-up or a
    mid-batch redeploy looks exactly like this from the master API -- and a reader who cannot tell the
    two apart ends up debugging the cluster instead of reading the log, which is how an afternoon goes.
    """
    print("%s -- %s" % (time.strftime("%H:%M:%S"), msg), flush=True)


try:
    d = json.load(open("/tmp/ls-tables.json"))
except Exception:
    note("master API unreadable (bring-up, redeploy, or master down)")
    sys.exit()

# The `user` key, not `tables`. The top level is {user, index, system}, so a lookup for "tables" yields
# nothing and reads as "no tables exist" -- it had me believing the cluster was down twice in one day.
seen = 0
for t in d.get("user", []):
    n = t.get("table_name") or ""
    if not n.startswith("ns_") or n.startswith("ns__"):
        continue
    seen += 1
    out = subprocess.run(["curl", "-sk", "-m", "10", f"{M}/api/v1/table?id={t['uuid']}"],
                         capture_output=True, text=True).stdout
    try:
        tabs = json.loads(out).get("tablets") or []
    except Exception:
        note(f"{n}: table detail unreadable")
        continue
    # Running only. A completed split leaves the parent in the array as Deleted, so counting entries
    # overstates the live layout -- the same trap that produced a "23 tablets" that was never 23.
    leaders = collections.Counter()
    running = 0
    for tab in tabs:
        if tab.get("state") != "Running":
            continue
        running += 1
        for loc in tab.get("locations") or []:
            if loc.get("role") == "LEADER":
                host = (loc.get("location") or "").split("//")[-1].split(":")[0]
                leaders[host] += 1
    if not running:
        note(f"{n}: no Running tablets yet")
        continue
    dist = ",".join(f"{h.rsplit('.', 1)[-1]}={c}" for h, c in sorted(leaders.items()))
    print("%s %s %d %d %d %s" % (time.strftime("%H:%M:%S"), n, running, len(leaders),
                                 max(leaders.values()) if leaders else 0, dist or "-"), flush=True)

if not seen:
    note("no ns_ table present")
PYEOF
  sleep 60
done
