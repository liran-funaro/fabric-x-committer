#!/bin/bash

echo "Downloading and extracting YugabyteDB"
mkdir -p "$HOME/bin"
curl https://downloads.yugabyte.com/releases/2.20.0.1/yugabyte-2.20.0.1-b1-linux-x86_64.tar.gz | tar -xz -C "$HOME/bin"
cd "$HOME/bin/yugabyte-2.20.0.1" || exit 1

echo "Installing YugabyteDB"
bin/post_install.sh

echo "Running YugabyteDB"
bin/yugabyted start \
	--advertise_address 0.0.0.0 \
	--callhome false \
	--fault_tolerance none \
	--background true \
	--ui false \
	--tserver_flags "ysql_max_connections=5000" \
	--insecure
