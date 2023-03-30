#!/bin/bash

signed_envs=$1
boosted=$2
bin_out=$3

cd "$FABRIC_PATH" || exit
git reset --hard
make clean

echo "Applying patch to accept MESSAGE type..."
git apply /usr/local/allow_MESSAGE_type.patch
if [ -n "$signed_envs" ] && [ "$signed_envs" = "false" ]; then \
  echo "Applying patch and building orderer binaries for unsigned envelopes..."
  git apply /usr/local/orderer_no_sig_check.patch
else \
  echo "Building orderer binaries for signed envelopes..."
fi
if [ -n "$boosted" ] && [ "$boosted" = "true" ]; then \
  echo "Applying booster patch and building orderer binaries..."
  git apply /usr/local/orderer_booster.patch
else \
  echo "Building orderer binaries without booster patch..."
fi
make native

echo "Bins created under $BINS_PATH and copying to $bin_out"
cp -a "$BINS_PATH/." "$bin_out/"
