#!/usr/bin/env bash

set -e

/home/yugabyte/bin/yugabyted start --advertise_address=0.0.0.0
"$BINS_PATH/signatureverifier" start --configs "$CONFIGS_PATH/config-sigservice.yaml" &
"$BINS_PATH/queryexecutor" start --configs "$CONFIGS_PATH/config-queryservice.yaml" &
"$BINS_PATH/validatorpersister" start --configs "$CONFIGS_PATH/config-vcservice.yaml" &
"$BINS_PATH/coordinator" start --configs "$CONFIGS_PATH/config-coordinator.yaml" &
"$BINS_PATH/sidecar" start --configs "$CONFIGS_PATH/config-sidecar.yaml"
