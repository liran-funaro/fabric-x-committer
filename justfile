#!/usr/bin/env just --justfile

### Constants

#default-deployment-files := "bin eval/deployments/default/*"

default := ''

# From where to copy the binaries/config files on the local host
bin-input-dir := "bin/"
config-input-dir := "config/"

# Where to copy the binaries/config files on the remote host
bin-remote-dir := "bin/"
config-remote-dir := ""

# Stores the temporary config files for the current experiment
experiment-config-dir := "eval/experiments/configs/"
# Each file in this folder keeps track of an experiment suite.
# Each line contains the experiment parameters, when it started and when we should sample.
experiment-tracking-dir := "eval/experiments/trackers/"
# Each file in this folder contains the results for an experiment suite.
# Each line corresponds one-to-one to the lines of the respective track file with the same name.
experiment-results-dir := "eval/experiments/results/"

# Experiment constants
experiment-duration-seconds := "600"

# Well-known ports
prometheus-scraper-port := "9091"
prometheus-exporter-port := "2112"
jaeger-exporter-port := "14268"
coordinator-port := "5002"

playbook-path := "./ansible/playbooks"
export ANSIBLE_CONFIG := "./ansible/ansible.cfg"

array-separator := ","

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
    go build -o {{bin-input-dir}}/blockgen ./wgclient/cmd/generator
    go build -o {{bin-input-dir}}/mockcoordinator ./wgclient/cmd/mockcoordinator

build-coordinator:
    go build -o {{bin-input-dir}}/coordinator ./coordinatorservice/cmd/server

build-sigservice:
    go build -o {{bin-input-dir}}/sigservice ./sigverification/cmd/server

build-shardsservice:
    go build -o {{bin-input-dir}}/shardsservice ./shardsservice/cmd/server

build-result-gatherer:
    go build -o {{bin-input-dir}}/resultgatherer ./utils/experiment/cmd

### Deploy

#transfer-all +files=(default-deployment-files):
#    just list-hosts | while read line; do just deploy "$line" {{files}}; done

#transfer host +files=(default-deployment-files):
#    rsync -P -r {{files}} root@{{host}}:~

# Lists all hostnames from the inventory
list-hosts name=("all"):
    ansible {{name}} --list-hosts | tail -n +2 | \
      while read line; do ansible-inventory --host $line | jq '.ansible_host' | sed -e 's/^"//' -e 's/"$//';done

# The docker image required for compilation on the Unix machines
docker-image:
    docker build -f builder/Dockerfile -t sc_builder .

# Executes a command from within the docker image. Requires that docker-image be run once before, to ensure the image exists.
docker CMD:
    docker run --rm -it -v "$PWD":/scalable-committer -w /scalable-committer sc_builder:latest {{CMD}}

deploy-base-setup:
    just docker "just build-all"
    just deploy-bins
    just deploy-base-configs

#
deploy-bins:
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['blockgen'], 'src_dir': '{{bin-input-dir}}', 'dst_dir': '{{bin-remote-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "{'target_hosts': 'coordinators', 'filenames': ['coordinator', 'mockcoordinator'], 'src_dir': '{{bin-input-dir}}', 'dst_dir': '{{bin-remote-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "{'target_hosts': 'sigservices', 'filenames': ['sigservice'], 'src_dir': '{{bin-input-dir}}', 'dst_dir': '{{bin-remote-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-copy-service-bin.yaml" --extra-vars "{'target_hosts': 'shardsservices', 'filenames': ['shardsservice'], 'src_dir': '{{bin-input-dir}}', 'dst_dir': '{{bin-remote-dir}}'}"

# Copies config/profile files from the local host to the corresponding remote servers
# Each server will receive only the files it needs
deploy-base-configs:
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['config-blockgen.yaml', 'profile-blockgen.yaml'], 'src_dir': '{{config-input-dir}}', 'dst_dir': '{{config-remote-dir}}'}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "{'target_hosts': 'coordinators', 'filenames': ['config-coordinator.yaml'], 'src_dir': '{{config-input-dir}}', 'dst_dir': '{{config-remote-dir}}'}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "{'target_hosts': 'sigservices', 'filenames': ['config-sigservice.yaml'], 'src_dir': '{{config-input-dir}}', 'dst_dir': '{{config-remote-dir}}'}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "{'target_hosts': 'shardsservices', 'filenames': ['config-shardsservice.yaml'], 'src_dir': '{{config-input-dir}}', 'dst_dir': '{{config-remote-dir}}'}"

### Experiments

run-variable-sigverifier-experiment:
    just run-experiment-suite "variable_sigverifiers" "1,2,3,4"
run-variable-shard-experiment:
    just run-experiment-suite "variable_shards" "3" "1,2,3,4,5,6"
run-variable-tx-sizes-experiment:
    just run-experiment-suite "variable_tx_sizes" "3" "3" "0.0,0.05,0.1,0.15,0.2,0.25,0.3"
run-variable-validity-ratio-experiment:
    just run-experiment-suite "variable_validity_ratios" "3" "3" "0.0" "0.0,0.05,0.1,0.15,0.2,0.25,0.3"
run-variable-double-spends-experiment:
    just run-experiment-suite "variable_double_spends" "3" "3" "0.0" "0.0" "0.0,0.05,0.1,0.15,0.2,0.25,0.3"
run-variable-block-size-experiment:
    just run-experiment-suite "variable_block_sizes" "3" "3" "0.0" "1.0" "50,100,3500,7000,15000,30000"

run-experiment-suite  experiment_name sig_verifiers_arr=("3") shard_servers_arr=("3") large_txs_arr=("0.0") invalidity_ratio_arr=("0.0") double_spends_arr=("0.0") block_sizes_arr=("100") experiment_duration=(experiment-duration-seconds):
    mkdir -p {{experiment-tracking-dir}}
    echo "sig_verifiers,shard_servers,large_txs,invalidity_ratio,double_spends,block_size,start_time,sample_time" > "{{experiment-tracking-dir}}/{{experiment_name}}.csv"; \
    echo {{sig_verifiers_arr}} | tr '{{array-separator}}' '\n' | while read sig_verifiers; do \
      echo {{shard_servers_arr}} | tr '{{array-separator}}' '\n' | while read shard_servers; do \
        echo {{large_txs_arr}} | tr '{{array-separator}}' '\n' | while read large_txs; do \
          echo {{invalidity_ratio_arr}} | tr '{{array-separator}}' '\n' | while read invalidity_ratio; do \
            echo {{double_spends_arr}} | tr '{{array-separator}}' '\n' | while read double_spends; do \
                echo {{block_sizes_arr}} | tr '{{array-separator}}' '\n' | while read block_size; do \
                    echo "Running experiment {{experiment_name}} for {{experiment_duration}} seconds. Settings:\n\t$sig_verifiers Sig verifiers\n\t$shard_servers Shard servers\n\t$large_txs/1 Large TXs\n\t$invalidity_ratio/1 Invalidity ratio\n\t$double_spends/1 Double spends\n\t$block_size TXs per block\nExperiment records are stored in {{experiment-tracking-dir}}/{{experiment_name}}.csv.\n"; \
                    just run-experiment $sig_verifiers $shard_servers $large_txs $invalidity_ratio $double_spends $block_size; \
                    echo $sig_verifiers,$shard_servers,$large_txs,$invalidity_ratio,$double_spends,$block_size,$(just get-timestamp 0 +%s),$(just get-timestamp {{experiment_duration}} +%s) >> "{{experiment-tracking-dir}}/{{experiment_name}}.csv"; \
                    echo "Waiting experiment {{experiment_name}} until $(just get-timestamp {{experiment_duration}}). Settings:\n\t$sig_verifiers Sig verifiers\n\t$shard_servers Shard servers\n\t$large_txs/1 Large TXs\n\t$invalidity_ratio/1 Invalidity ratio\n\t$double_spends/1 Double spends\n\t$block_size TXs per block\nExperiment records are stored in {{experiment-tracking-dir}}/{{experiment_name}}.csv.\n"; \
                    sleep {{experiment_duration}} \
                  ;done \
              ;done \
          ;done \
        ;done \
      ;done \
    done
    just gather-results "{{experiment-tracking-dir}}{{experiment_name}}.csv" "{{experiment-results-dir}}{{experiment_name}}.csv"

run-experiment sig_verifiers=("3") shard_servers=("3") large_txs=("0.0") invalidity_ratio=("0.0") double_spends=("0.0") block_size=("100"):
    ansible-playbook "{{playbook-path}}/60-create-experiment-configs.yaml" --extra-vars "{'src_config_dir': ../../{{config-input-dir}}, 'dst_config_dir': ../../{{experiment-config-dir}}, 'sig_verifiers': {{sig_verifiers}}, 'shard_servers': {{shard_servers}}, 'large_txs': {{large_txs}}, 'small_txs': $(bc <<< "1 - {{large_txs}}"), 'invalidity_ratio': {{invalidity_ratio}}, 'double_spends': {{double_spends}}, 'block_size': {{block_size}}, 'coordinator_endpoint': $(just list-hosts coordinators):{{coordinator-port}}}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['config-blockgen.yaml', 'profile-blockgen.yaml'], 'src_dir': '{{experiment-config-dir}}', 'dst_dir': '{{config-remote-dir}}'}"
    ansible-playbook "{{playbook-path}}/20-copy-service-config.yaml" --extra-vars "{'target_hosts': 'coordinators', 'filenames': ['config-coordinator.yaml'], 'src_dir': '{{experiment-config-dir}}', 'dst_dir': '{{config-remote-dir}}'}"
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"sig_verifiers": {{sig_verifiers}}, "shard_servers": {{shard_servers}}}'

# Goes through all of the entries of the tracker file and retrieves the corresponding metric for each line (as defined at the sampling-time field)
gather-results tracker_file result_file:
    mkdir -p {{experiment-results-dir}}
    {{bin-input-dir}}resultgatherer -client-endpoint=$(just list-hosts blockgens):{{prometheus-exporter-port}} -prometheus-endpoint=$(just list-hosts monitoring):{{prometheus-scraper-port}} -output={{result_file}} -rate-interval=2m -sampling-times=$(cat {{tracker_file}} | tail -n +2 | while read line; do echo ${line##*,};done | tr '\n' ',')

# Unix and OSX have different expressions to retrieve the timestamp
get-timestamp plus_seconds=("0") format=(""):
    #date --date="+{{plus_seconds}} seconds" +%s #bash
    date -v +{{plus_seconds}}S {{format}} #osx
