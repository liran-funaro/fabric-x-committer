#!/usr/bin/env bash
#
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
# Switch the cluster from the real-orderer arm to the committer-only arm and run the paper's
# committer figures on it. Runs ON the monitor, detached: the whole sequence is one process so
# that losing the controlling session does not leave a half-switched cluster.
#
# The committer-only arm is the one to measure here. The paper's Section 6.3 evaluates the
# validation phase with its own workload generator and no ordering service in the path, which is
# exactly what cluster.yaml deploys.
set -u -o pipefail

source /data1/cluster/bin/fx-env.sh
cd "$FX_PROJECT" || exit 1

ORDERER=/data1/cluster/inventory/cluster-orderer.yaml
COMMITTER=/data1/cluster/inventory/cluster.yaml

if [ "${FX_SKIP_TEARDOWN:-0}" = "1" ]; then
  echo "=== $(date +%H:%M:%S) skipping the real-orderer teardown (already done)"
else
  echo "=== $(date +%H:%M:%S) tearing down the real-orderer arm"
  ANSIBLE_INVENTORY=$ORDERER make teardown || echo "!! teardown returned $?, continuing"
fi

# Tearing down drops the Fabric CA's database, but the admin's enrolled MSP is on disk in the
# CA's deploy directory and outlives it, so the next enrollment presents a certificate the fresh
# registry has never seen and crypto generation stops at "Authentication failure". The CA's own
# state has to go with its database.
echo "=== $(date +%H:%M:%S) wiping the Fabric CA's stale enrollment state"
ANSIBLE_INVENTORY=$COMMITTER make hard-wipe TARGET_HOSTS=fabric_cas || echo "!! CA wipe returned $?"

# committer_build_bin is false, so the committer and loadgen binaries are built on the
# workstation and staged into the collection's out/ tree; a setup run empties that tree before
# the transfer play reads from it, and then fails every host with "could not find ... on the
# Ansible Controller". Restoring them from a copy kept outside out/ makes setup repeatable.
echo "=== $(date +%H:%M:%S) restoring the staged committer binaries"
install -m 0750 -D /data1/bin-stage/committer /data1/bin-stage/loadgen \
  -t "$FX_PROJECT/out/control-node/bin/Linux/x86_64/" || echo "!! staging restore returned $?"

echo "=== $(date +%H:%M:%S) setting up the committer-only arm"
# Regenerating crypto invalidates Grafana's stored datasource CA, so its container is recreated
# rather than restarted -- which is what teardown plus start does below, via the driver.
ANSIBLE_INVENTORY=$COMMITTER make setup || { echo "!! setup failed"; exit 1; }

echo "=== $(date +%H:%M:%S) running the figures"
exec /data1/cluster/bin/fx-figures.py "${1:-}"
