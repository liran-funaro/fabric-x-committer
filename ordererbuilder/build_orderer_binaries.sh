#!/bin/bash

signed_envs=$1
bin_out=$2

cd "$FABRIC_PATH" || exit
git reset --hard
if [ -d build ]; then \
      rm -r build; \
    fi
if [ -n "$signed_envs" ] && [ "$signed_envs" = "false" ]; then \
  echo "Applying patch and building orderer binaries for unsigned envelopes..."
  git apply /usr/local/orderer_no_sig_check.patch
else \
  echo "Building orderer binaries for signed envelopes..."
fi
make native

echo "Bins created under $BINS_PATH"

if [ -n "$bin_out" ]; then \
  echo "Copying bins to $bin_out..."
  cp -a "$BINS_PATH/." "$bin_out/"
fi