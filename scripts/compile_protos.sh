#!/bin/bash

rootDir="$(dirname "$(pwd)")"
PROTO_DIRS="$(find "$rootDir" \
    -name '*.proto' -print0 | \
    xargs -0 -n 1 dirname)"

for dir in ${PROTO_DIRS}; do
    protoc -I="$rootDir" \
        --go_out="$rootDir" --go_opt=paths=source_relative \
        --go-grpc_out="$rootDir" --go-grpc_opt=paths=source_relative \
        "$dir"/*.proto
done