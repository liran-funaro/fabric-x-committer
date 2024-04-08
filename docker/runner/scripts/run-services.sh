#!/bin/bash

if [[ -z "${SIGSERVICE_WAIT}" ]]; then SIGSERVICE_WAIT=5; fi
if [[ -z "${YUGABYTE_WAIT}" ]]; then YUGABYTE_WAIT=5; fi
if [[ -z "${DB_INIT_WAIT}" ]]; then DB_INIT_WAIT=5; fi
if [[ -z "${VCSERVICE_WAIT}" ]]; then VCSERVICE_WAIT=10; fi
if [[ -z "${COORDINATOR_WAIT}" ]]; then COORDINATOR_WAIT=10; fi
if [[ -z "${COORDINATOR_SETUP_WAIT}" ]]; then COORDINATOR_SETUP_WAIT=5; fi

"$BINS_PATH/sigservice" --configs "$CONFIGS_PATH/config-sigservice.yaml" &
sleep $SIGSERVICE_WAIT
/home/yugabyte/bin/yugabyted start --advertise_address=0.0.0.0
sleep $YUGABYTE_WAIT
"$BINS_PATH/vcservice" init --configs "$CONFIGS_PATH/config-vcservice.yaml" --namespaces 0
sleep $DB_INIT_WAIT
"$BINS_PATH/vcservice" start --configs "$CONFIGS_PATH/config-vcservice.yaml" &
sleep $VCSERVICE_WAIT
"$BINS_PATH/coordinator" start --configs "$CONFIGS_PATH/config-coordinator.yaml" &
sleep $COORDINATOR_WAIT
"$BINS_PATH/coordinator_setup" --coordinator :5002 --key-path "$PUBKEY_PATH/sc_pubkey.pem" --scheme "$METANS_SIG_SCHEME"
sleep $COORDINATOR_SETUP_WAIT
"$BINS_PATH/sidecar" --configs "$CONFIGS_PATH/config-sidecar.yaml"
