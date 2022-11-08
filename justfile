#!/usr/bin/env just --justfile

### Constants

default := ''
bin-build-out := "bin"
config-build-out := "eval/deployments/default"
default-deployment-files := "bin eval/deployments/default/*"
experiment-tracking-dir := "eval/experiments/trackers"
experiment-results-dir := "eval/experiments/results"
playbook-path := "./ansible/playbooks"
array-separator := ","
sig-verifiers-arr := "3"
experiment-duration-seconds := "600"
prometheus-endpoint := "9094"

export ANSIBLE_CONFIG := "./ansible/ansible.cfg"

### Build

build:
	./scripts/build_all.sh

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


build-all: build-blockgen build-coordinator build-sigservice build-shardsservice build-result-gatherer

build-blockgen:
    go build -o {{bin-build-out}}/blockgen ./wgclient/cmd/generator
#    go build -o {{bin-build-out}}/mockcoordinator ./wgclient/cmd/mockcoordinator

build-coordinator:
    go build -o {{bin-build-out}}/coordinator ./coordinatorservice/cmd/server

build-sigservice:
    go build -o {{bin-build-out}}/sigservice ./sigverification/cmd/server

build-shardsservice:
    go build -o {{bin-build-out}}/shardsservice ./shardsservice/cmd/server

build-result-gatherer:
    go build -o {{bin-build-out}}/resultgatherer ./utils/experiment/cmd

### Deploy

transfer-all +files=(default-deployment-files):
    just list-hosts | while read line; do just deploy "$line" {{files}}; done

list-hosts name=("all"):
    ansible {{name}} --list-hosts | tail -n +2 | \
      while read line; do ansible-inventory --host $line | jq '.ansible_host' | sed -e 's/^"//' -e 's/"$//';done

transfer host +files=(default-deployment-files):
    rsync -P -r {{files}} root@{{host}}:~

docker-image:
    docker build -f builder/Dockerfile -t sc_builder .

docker CMD:
    docker run --rm -it -v "$PWD":/scalable-committer -w /scalable-committer sc_builder:latest {{CMD}}

deploy-base-setup:
    just docker "just build-all"
    just deploy-bins
    just gather-configs
    just deploy-configs

deploy-bins:
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "servicename=blockgen"
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "servicename=coordinator"
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "servicename=shardsservice"
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "servicename=sigservice"

gather-configs dstpath=(config-build-out):
    mkdir -p {{dstpath}}
    cp ./config/config-*.yaml {{dstpath}}

deploy-configs configpath=(config-build-out):
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "servicename=blockgen" --extra-vars "configpath={{configpath}}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "servicename=coordinator" --extra-vars "configpath={{configpath}}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "servicename=shardsservice" --extra-vars "configpath={{configpath}}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "servicename=sigservice" --extra-vars "configpath={{configpath}}"

### Experiments

run-variable-shard-experiment:
    just run-experiment-suite "variable_shards" "1,2,3,4,5,6,7,8,9,10"
run-variable-tx-sizes-experiment:
    just run-experiment-suite "variable_tx_sizes" "3" "0.0,0.05,0.1,0.15,0.2,0.25,0.3"
run-variable-validity-ratio-experiment:
    just run-experiment-suite "variable_tx_sizes" "3" "0.0" "0.0,0.05,0.1,0.15,0.2,0.25,0.3"
run-variable-double-spends-experiment:
    just run-experiment-suite "variable_double_spends" "3" "0.0" "0.0" "0.0,0.05,0.1,0.15,0.2,0.25,0.3"
run-variable-block-size-experiment:
    just run-experiment-suite "variable_block_sizes" "3" "0.0" "1.0" "50,100,3500,7000,15000,30000"

run-experiment-suite  experiment_name shard_servers_arr=("3") large_txs_arr=("0.0") invalidity_ratio_arr=("0.0") double_spends_arr=("0.0") block_sizes_arr=("100") experiment_duration=(experiment-duration-seconds):
    mkdir -p {{experiment-tracking-dir}}
    echo "sig_verifiers,shard_servers,large_txs,invalidity_ratio,double_spends,block_size,start_time,sample_time" > "{{experiment-tracking-dir}}/{{experiment_name}}.csv"; \
    echo {{sig-verifiers-arr}} | tr '{{array-separator}}' '\n' | while read sig_verifiers; do \
      echo {{shard_servers_arr}} | tr '{{array-separator}}' '\n' | while read shard_servers; do \
        echo {{large_txs_arr}} | tr '{{array-separator}}' '\n' | while read large_txs; do \
          echo {{invalidity_ratio_arr}} | tr '{{array-separator}}' '\n' | while read invalidity_ratio; do \
            echo {{double_spends_arr}} | tr '{{array-separator}}' '\n' | while read double_spends; do \
                echo {{block_sizes_arr}} | tr '{{array-separator}}' '\n' | while read block_size; do \
                    echo "Running experiment {{experiment_name}} for {{experiment_duration}} seconds. Settings:\n\t$sig_verifiers Sig verifiers\n\t$shard_servers Shard servers\n\t$large_txs/1 Large TXs\n\t$invalidity_ratio/1 Invalidity ratio\n\t$double_spends/1 Double spends\n\t$block_size TXs per block\nExperiment records are stored in {{experiment-tracking-dir}}/{{experiment_name}}.csv.\n"; \
                    just run-experiment $sig_verifiers $shard_servers $large_txs $invalidity_ratio $double_spends $block_size; \
                    echo $sig_verifiers,$shard_servers,$large_txs,$invalidity_ratio,$double_spends,$block_size,$(just get-timestamp 0 +%s),$(just get-timestamp {{experiment_duration}} +%s) >> "{{experiment-tracking-dir}}/{{experiment_name}}.txt"; \
                    echo "Waiting experiment {{experiment_name}} until $(just get-timestamp {{experiment_duration}}). Settings:\n\t$sig_verifiers Sig verifiers\n\t$shard_servers Shard servers\n\t$large_txs/1 Large TXs\n\t$invalidity_ratio/1 Invalidity ratio\n\t$double_spends/1 Double spends\n\t$block_size TXs per block\nExperiment records are stored in {{experiment-tracking-dir}}/{{experiment_name}}.csv.\n"; \
                    sleep {{experiment_duration}} \
                  ;done \
              ;done \
          ;done \
        ;done \
      ;done \
    done
    just gather-results "{{experiment-tracking-dir}}/{{experiment_name}}.csv" "{{experiment-results-dir}}/{{experiment_name}}.csv"

run-experiment sig_verifiers=("3") shard_servers=("3") large_txs=("0.0") invalidity_ratio=("0.0") double_spends=("0.0") block_size=("100"):
    ansible-playbook "{{playbook-path}}/60-config-experiment.yaml" --extra-vars "{'src_config_dir': {{config-build-out}}, 'sig_verifiers': {{sig_verifiers}}, 'shard_servers': {{shard_servers}}, 'large_txs': {{large_txs}}, 'small_txs': $(bc <<< "1 - {{large_txs}}"), 'invalidity_ratio': {{invalidity_ratio}}, 'double_spends': {{double_spends}}, 'block_size': {{block_size}}}"
    ansible-playbook "{{playbook-path}}/70-run-experiment.yaml" --extra-vars '{"sig_verifiers": {{sig_verifiers}}, "shard_servers": {{shard_servers}}}'

gather-results tracker_file result_file:
    mkdir -p {{experiment-results-dir}}
    {{bin-build-out}}/resultgatherer -prometheus-endpoint=$(just list-hosts monitoring):{{prometheus-endpoint}} -output={{result_file}} -sampling-times=$(cat {{tracker_file}} | tail -n +2 | while read line; do echo ${line##*,};done | tr '\n' ',')

get-timestamp plus_seconds=("0") format=(""):
    #date --date="+{{plus_seconds}} seconds" +%s #bash
    date -v +{{plus_seconds}}S {{format}} #osx
