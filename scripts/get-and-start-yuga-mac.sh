#!/bin/bash


# yugabyted scripts require "python" command.
# If your system only have python3 command, use
# ln -s "$(brew --prefix)/bin/python"{3,}

YUGA_DIR=${YUGA_DIR:="$HOME/Library/bin/yugabyte"}

if [ ! -d "$YUGA_DIR" ]; then
  echo "Downloading and extracting YugabyteDB"
  mkdir -p "$YUGA_DIR"
  curl https://downloads.yugabyte.com/releases/2.20.2.3/yugabyte-2.20.2.3-b2-darwin-x86_64.tar.gz |
          tar --strip=1 -xz -C "$YUGA_DIR"
fi

cd "$YUGA_DIR" || exit 1

DATA_DIR=$(mktemp -d -t yuga)
echo "Using temporary data dir: $DATA_DIR"

./bin/yugabyted start \
  --advertise_address 0.0.0.0 \
	--callhome false \
	--fault_tolerance none \
	--background false \
	--ui false \
	--tserver_flags "ysql_max_connections=5000" \
	--insecure \
	--base_dir "$DATA_DIR"

echo "Clearing temporary data dir: $DATA_DIR"
rm -rf "$DATA_DIR"
