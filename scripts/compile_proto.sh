#!/bin/bash

set -eux -o pipefail

# Find all proto dirs to be processed
PROTO_DIRS="$(find "$(pwd)" \
    -path "$(pwd)/protos" -prune -o \
    -path "$(pwd)/wgclient" -prune -o \
    -name '*.proto' -print0 | \
    xargs -0 -n 1 dirname | \
    sort -u | grep -v testdata)"

BASE_DIR="$(pwd)"

for dir in ${PROTO_DIRS}; do
    protoc --proto_path="$BASE_DIR" \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
        --go_out=paths=source_relative:. "$dir"/*.proto
done
