#!/bin/bash

echo "Downloading and extracting YugabyteDB"
mkdir -p "$HOME/bin/yugabyte"
curl https://downloads.yugabyte.com/releases/2.20.7.0/yugabyte-2.20.7.0-b58-linux-x86_64.tar.gz |
  tar --strip=1 -xz -C "$HOME/bin/yugabyte"
cd "$HOME/bin/yugabyte" || exit 1

echo "Installing YugabyteDB"
bin/post_install.sh

echo "Unlimited open files and enabling transparent huge pages..."
ulimit -n unlimited
echo always | sudo tee /sys/kernel/mm/transparent_hugepage/enabled
echo always | sudo tee /sys/kernel/mm/transparent_hugepage/defrag

echo "Running YugabyteDB"
bin/yugabyted start \
  --advertise_address 0.0.0.0 \
  --callhome false \
  --fault_tolerance none \
  --background true \
  --ui false \
  --tserver_flags "ysql_max_connections=5000" \
  --insecure
