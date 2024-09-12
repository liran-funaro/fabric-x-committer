#!/usr/bin/env bash

set -e

"$BINS_PATH/sigservice" start --configs "$CONFIGS_PATH/config-sigservice.yaml" &
/home/yugabyte/bin/yugabyted start --advertise_address=0.0.0.0

"$BINS_PATH/queryservice" start --configs "$CONFIGS_PATH/config-queryservice.yaml" &

"$BINS_PATH/vcservice" init --configs "$CONFIGS_PATH/config-vcservice.yaml" --namespaces 0
"$BINS_PATH/vcservice" start --configs "$CONFIGS_PATH/config-vcservice.yaml" &

# wait until sigservice and vcservice are ready before we start coordinator
while !</dev/tcp/localhost/4001; do sleep 1; done;
while !</dev/tcp/localhost/5001; do sleep 1; done;
"$BINS_PATH/coordinator" start --configs "$CONFIGS_PATH/config-coordinator.yaml" &

# wail until coordinator is up and running before call setup and start the sidecar
while !</dev/tcp/localhost/5002; do sleep 1; done;

"$BINS_PATH/coordinator_setup" --coordinator :5002 --key-path "$METANS_SIG_VERIFICATION_KEY_PATH" --scheme "$METANS_SIG_SCHEME"
"$BINS_PATH/sidecar" start --configs "$CONFIGS_PATH/config-sidecar.yaml"
