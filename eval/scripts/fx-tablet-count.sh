#!/usr/bin/env bash
# Record the state table's tablet count every 60 s. Read-only: master API calls only.
#
# It exists because `pre_split_tablets: 0` does NOT mean one tablet and does not mean many: it omits
# SPLIT INTO, so YugabyteDB picks -- and here it picked FIVE across twelve tablet servers. That is 0.42 per
# node, which puts the table in automatic splitting's LOW phase, threshold 128 MiB, rather than the high
# phase's 10 GiB. A 300 s rung at 15,000 tps writes ~180 MB per tablet. So the count is expected to move
# DURING a hold, and a ladder that assumes a fixed layout would be measuring a moving one.
set -u
M=https://10.241.64.10:5310
echo "ts table tablets sst_bytes"
while true; do
  curl -sk -m 10 "$M/api/v1/tables" 2>/dev/null > /tmp/tc-tables.json || true
  python3 - "$M" <<'PY'
import json, subprocess, sys, time
M = sys.argv[1]
try:
    d = json.load(open("/tmp/tc-tables.json"))
except Exception:
    sys.exit()
for t in d.get("user", []):
    n = t.get("table_name") or ""
    if not n.startswith("ns_") or n.startswith("ns__"):
        continue
    sst = (t.get("on_disk_size") or {}).get("sst_files_size_bytes", 0)
    out = subprocess.run(["curl", "-sk", "-m", "10", f"{M}/api/v1/table?id={t['uuid']}"],
                         capture_output=True, text=True).stdout
    try:
        cnt = len(json.loads(out).get("tablets") or [])
    except Exception:
        cnt = "-"
    print(time.strftime("%H:%M:%S"), n, cnt, sst, flush=True)
PY
  sleep 60
done
