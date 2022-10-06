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
    go build -o {{bin-build-out}}/mockcoordinator ./wgclient/cmd/mockcoordinator

build-coordinator:
    go build -o {{bin-build-out}}/coordinator ./coordinatorservice/cmd/server

build-sigservice:
    go build -o {{bin-build-out}}/sigservice ./sigverification/cmd/server

build-shardsservice:
    go build -o {{bin-build-out}}/shardsservice ./shardsservice/cmd/server

docker-image:
    docker build -f builder/Dockerfile -t sc_builder .

docker CMD:
    docker run --rm -it -v "$PWD":/scalable-committer -w /scalable-committer sc_builder {{CMD}}

defaultDeploymentFiles := "bin eval/deployment/default/*"

deploy-all hosts=("ansible/inventory/hosts") +files=(defaultDeploymentFiles):
    # just create ansible/inventory/hosts with a list of servers
    cat {{hosts}} | while read line; do just deploy $line {{files}}; done

deploy host +files=(defaultDeploymentFiles):
    rsync -P -r {{files}} root@{{host}}:~

playbook-path := "./ansible/playbooks"
export ANSIBLE_CONFIG := "./ansible/ansible.cfg"

deploy-bins:
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "servicename=blockgen"
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "servicename=coordinator"
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "servicename=shardsservice"
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "servicename=sigservice"

deploy-configs configpath=("eval/deployments/default"):
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "servicename=coordinator" --extra-vars "configpath={{configpath}}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "servicename=shardsservice" --extra-vars "configpath={{configpath}}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "servicename=sigservice" --extra-vars "configpath={{configpath}}"
