#!/usr/bin/env just --justfile

#########################
# Constants
#########################

project-dir := env_var_or_default('PWD', '.')
runner-dir := project-dir + "/docker/runner"
sc-builder-dir := project-dir + "/docker/builder"
local-bin-output-dir := project-dir + "/out/local"
linux-bin-output-dir := project-dir + "/out/linux"

#########################
# Quickstart
#########################

build:
    just build-local
    just build-linux

test:
    go test -v ./...

#########################
# Generate protos
#########################

protos-coordinator:
    protoc \
    --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    --proto_path=. \
    --proto_path=./token \
    --proto_path=./coordinatorservice \
    ./coordinatorservice/coordinator_service.proto

protos-token:
    protoc \
    --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    --proto_path=. \
    --proto_path=./token \
    ./token/token.proto

protos-wgclient:
    protoc \
    --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    --proto_path=. \
    --proto_path=./token \
    --proto_path=./coordinatorservice \
    ./wgclient/workload/expected_results.proto

#########################
# Binaries
#########################

build-linux output_dir=(linux-bin-output-dir):
    just docker-builder-run "just build-local {{output_dir}}"

build-local output_dir=(local-bin-output-dir):
    mkdir -p output_dir
    go build -buildvcs=false -o {{output_dir}}/blockgen ./wgclient/cmd/generator
    go build -buildvcs=false -o {{output_dir}}/mockcoordinator ./wgclient/cmd/mockcoordinator
    go build -buildvcs=false -o {{output_dir}}/coordinator ./coordinatorservice/cmd/server
    go build -buildvcs=false -o {{output_dir}}/coordinator_setup ./coordinatorservice/cmd/setup_helper
    go build -buildvcs=false -o {{output_dir}}/sigservice ./sigverification/cmd/server
    go build -buildvcs=false -o {{output_dir}}/shardsservice ./shardsservice/cmd/server
    go build -buildvcs=false -o {{output_dir}}/sidecar ./sidecar/cmd/server
    go build -buildvcs=false -o {{output_dir}}/sidecarclient ./wgclient/cmd/sidecarclient

# Executes a command from within the docker image. Requires that docker-builder-image be run once before, to ensure the image exists.
docker-builder-run CMD:
    docker run --rm -it -v "{{project-dir}}":{{project-dir}} -w {{project-dir}} sc_builder:latest {{CMD}}

# The docker image required for compilation of SC components on the Unix machines
docker-builder-image:
    docker build -f {{sc-builder-dir}}/Dockerfile -t sc_builder .

# Simple containerized SC
docker-runner-image:
    just build-linux {{runner-dir}}/out/bin
    docker build -f {{runner-dir}}/Dockerfile -t sc_runner .
