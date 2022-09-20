#!/usr/bin/env just --justfile

build:
	./scripts/build_all.sh

default := ''

test:
    go test -v ./...

run arg=default:
	./scripts/copy_and_run.sh {{arg}}

start arg=default: build (run arg)

protos-coordinator:
    protoc \
    --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    --proto_path=. \
    --proto_path=./token \
    --proto_path=./coordinatorservice \
    ./coordinatorservice/coordinator_service.proto
