#!/bin/bash

crypto_config=$1
txgen_config=$2
out=$3
channel_id=$4

"$BINS_PATH"/cryptogen generate \
    --config="$crypto_config" \
    --output="$out/orgs"

cp "$txgen_config" /usr/local/configtx.yaml

"$BINS_PATH"/configtxgen \
    -outputBlock "$out/genesisblock" \
    -profile SampleDevModeEtcdRaft \
    -channelID "$channel_id"

rm /usr/local/configtx.yaml