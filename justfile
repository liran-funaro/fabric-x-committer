#!/usr/bin/env just --justfile

build:
	./scripts/build_all.sh

default := ''

test:
    go test -v ./...

setup:
    ./scripts/setup.sh

run arg=default:
	./scripts/run.sh {{arg}}

start arg=default: build setup (run arg)

protos-coordinator:
    protoc \
    --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    --proto_path=. \
    --proto_path=./token \
    --proto_path=./coordinatorservice \
    ./coordinatorservice/coordinator_service.proto

bin-build-out := "bin"

build-all: build-blockgen build-coordinator build-sigservice build-shardsservice

build-blockgen:
    go build -o {{bin-build-out}}/blockgen ./wgclient/cmd/generator

build-coordinator:
    go build -o {{bin-build-out}}/coordinator ./coordinatorservice/cmd/server

build-sigservice:
    go build -o {{bin-build-out}}/sigservice ./sigverification/cmd/server

build-shardsservice:
    go build -o {{bin-build-out}}/shardservice ./shardsservice/cmd/server

docker-image:
    docker build -f builder/Dockerfile -t sc_builder .

docker CMD:
    docker run --rm -it -v "$PWD":/scalable-committer -w /scalable-committer sc_builder {{CMD}}
