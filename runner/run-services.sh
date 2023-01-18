#!/bin/bash

"$BINS_PATH/sigservice" --configs "$CONFIGS_PATH/sigservice/sigservice-machine-config-sigservice.yaml" &
"$BINS_PATH/shardsservice" --configs "$CONFIGS_PATH/shardsservice/shardsservice-machine-config-shardsservice.yaml" &
sleep 1 # Wait until the services are up and running before starting the coordinator
"$BINS_PATH/coordinator" --configs "$CONFIGS_PATH/coordinator/coordinator-machine-config-coordinator.yaml" &
sleep 5
"$BINS_PATH/sidecar" --configs "$CONFIGS_PATH/sidecar/sidecar-machine-config-sidecar.yaml" --orderer-config-path "$ORDERER_CONFIGS_PATH" --orderer-creds-path "$ORDERER_CONFIGS_PATH"
