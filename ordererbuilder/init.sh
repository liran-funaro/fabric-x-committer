#!/bin/bash

crypto_config=$1
txgen_config=$2
out=$3
channel_id=$4

echo "Parameters passed: '$crypto_config', '$txgen_config', '$out', '$channel_id'"

if [ -n "$crypto_config" ]; then \
  echo "Executing cryptogen..."
  "$BINS_PATH"/cryptogen generate \
    --config="$crypto_config" \
    --output="$out/orgs"
else \
  echo "Skipping cryptogen..."
fi

if [ -n "$txgen_config" ] || [ -n "$channel_id" ]; then \
  echo "Executing blockgen creation..."
  cp "$txgen_config" /usr/local/configtx.yaml
  "$BINS_PATH"/configtxgen \
      -outputBlock "$out/genesisblock" \
      -profile SampleDevModeEtcdRaft \
      -channelID "$channel_id"
  rm /usr/local/configtx.yaml
else \
  echo "Skipping blockgen creation..."
fi

