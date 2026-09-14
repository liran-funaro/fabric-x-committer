#!/usr/bin/env bash
# Run fx-explain-insert.sql against a tserver and print the plans. Answers eval TODO item 2d and
# pre-check 1 for the insert_ns A/B: does detecting a primary-key conflict read once or once per key?
#
# Refuses to run while a measurement is in flight. EXPLAIN ANALYZE executes, and the probe creates a
# 120-tablet table and writes ~4,400 rows, which is small but not free -- three benchmark runs have
# already been lost to exactly this kind of contamination.
#
# The SQL compares both forms directly on one table, so this does not care which binary is deployed;
# one run answers the question. ysql is on 5320, not 5433, and a tserver-local connection
# authenticates on trust, so no credential is read or passed.
set -u
SQL=${1:-$(dirname "$0")/fx-explain-insert.sql}
YB=/data1/fabric-x/yugabyte/yugabyte-2025.2.1.0/bin/ysqlsh

# The guard has to ask whichever machine runs the chain, not necessarily this one: from a workstation a
# local pgrep finds nothing and the guard would pass while a batch is mid-hold. Default is to ask the
# control node over ssh; pass MONITOR=local when already running there. The control node cannot ssh to
# itself -- neither `monitor` nor `localhost` has a known host key -- so `local` is not an optimisation,
# it is the only thing that works from there.
MONITOR=${MONITOR:-monitor}
# `bc` is not installed on either machine, so sum in the shell. Note pgrep -c exits 1 when it counts
# zero, which would abort the remote command list under -e; there is no -e here, and the guard fails
# closed on an unreadable answer anyway.
if [ "$MONITOR" = "local" ]; then
  BUSY=$(( $(pgrep -cf "[f]x-figures.py") + $(pgrep -cf "/[a]nsible-playbook ") ))
else
  BUSY=$(ssh -o ConnectTimeout=10 "$MONITOR" \
    'echo $(( $(pgrep -cf "[f]x-figures.py") + $(pgrep -cf "/[a]nsible-playbook ") ))' 2>/dev/null)
fi
if ! [ "${BUSY:-}" = "0" ]; then
  echo "!! a measurement or a deploy is in flight on $MONITOR (or it could not be reached); not running the probe" >&2
  exit 1
fi

for TS in 10.241.64.13 10.241.64.14 10.241.64.15; do
  if ssh -o ConnectTimeout=10 "$TS" "test -x $YB" 2>/dev/null; then
    echo "### probe on $TS at $(date +%H:%M:%S)"
    ssh "$TS" "$YB -h $TS -p 5320 -U yugabyte -d yugabyte -f -" < "$SQL" 2>&1
    exit $?
  fi
done
echo "!! no tserver answered" >&2
exit 1
