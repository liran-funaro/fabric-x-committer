#!/usr/bin/env bash
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
# Characterise a cluster node's disk. Copy to the node and run it there:
#   scp eval/scripts/fx-disk-bench.sh commit1:/tmp/ && ssh commit1 bash /tmp/fx-disk-bench.sh
#
# Needs fio, which the workers' mirror provides: sudo dnf install -y fio
# Four fio jobs, each shaped like an I/O pattern this deployment actually produces. Run on /data2 -- the
# second data disk, provisioned on every node and used by nothing -- so no live data is touched. Same
# device class, filesystem and mount options as /data1.
#
#  seq-append   1 MiB sequential writes, depth 4   -- sidecar and assembler ledger append
#  wal-fsync    4 KiB sequential writes, O_DSYNC   -- a consenter's write-ahead log
#  rand-read    8 KiB random reads, depth 32       -- YugabyteDB serving read validation
#  rand-write   8 KiB random writes, depth 32      -- YugabyteDB commits and compaction
#
# libaio, not the default psync: with a synchronous engine fio caps the queue depth at 1 and reports the
# requested depth anyway, so a "depth 32" line would have been a depth-1 measurement.
set -u
DIR=/data2/fiotest
mkdir -p "$DIR" || exit 1
common="--directory=$DIR --direct=1 --ioengine=libaio --size=2G --runtime=20 --time_based
         --group_reporting --output-format=json --output=$DIR/out.json"
run() {
  name=$1; shift
  fio --name="$name" $common "$@" >/dev/null 2>&1
  python3 -c '
import json,sys
d=json.load(open(sys.argv[1]))["jobs"][0]
for side in ("read","write"):
    s=d[side]
    if s["io_bytes"]:
        p=s["clat_ns"]["percentile"]
        print("%-11s %-5s %8.1f MiB/s %8.0f IOPS   p50 %7.0f us   p99 %8.0f us" % (
            d["jobname"], side, s["bw"]/1024, s["iops"],
            p.get("50.000000",0)/1000, p.get("99.000000",0)/1000))' "$DIR/out.json"
}
run seq-append  --rw=write     --bs=1M --iodepth=4
run wal-fsync   --rw=write     --bs=4k --iodepth=1  --sync=1
run rand-read   --rw=randread  --bs=8k --iodepth=32
run rand-write  --rw=randwrite --bs=8k --iodepth=32
rm -rf "$DIR"
