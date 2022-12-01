#!/usr/bin/env just --justfile

### Constants

#default-deployment-files := "bin eval/deployments/default/*"

default := ''

config-input-dir := "config/"

output-dir := "eval/"


deployment-dir := output-dir + "deployments/"

# Stores the configs that are deployed to the servers
base-setup-config-dir := deployment-dir + "configs/"
# Stores the binaries that are deployed to the servers
bin-input-dir := deployment-dir + "bins/"
osx-bin-input-dir := bin-input-dir + "osx/"
linux-bin-input-dir := bin-input-dir + "linux/"

experiment-dir := output-dir + "experiments/"
# Each file in this folder keeps track of an experiment suite.
# Each line contains the experiment parameters, when it started and when we should sample.
experiment-tracking-dir := experiment-dir + "trackers/"
# Each file in this folder contains the results for an experiment suite.
# Each line corresponds one-to-one to the lines of the respective track file with the same name.
experiment-results-dir := experiment-dir + "results/"

# Experiment constants
experiment-duration-seconds := "600"

# Well-known ports
prometheus-scraper-port := "9091"

playbook-path := "./ansible/playbooks"
export ANSIBLE_CONFIG := "./ansible/ansible.cfg"

sampling-time-header := "sample_time"
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


build-all output_dir:
    just build-blockgen {{output_dir}}
    just build-coordinator {{output_dir}}
    just build-sigservice {{output_dir}}
    just build-shardsservice {{output_dir}}
    just build-result-gatherer {{output_dir}}

build-blockgen output_dir:
    go build -o {{output_dir}}blockgen ./wgclient/cmd/generator
    go build -o {{output_dir}}mockcoordinator ./wgclient/cmd/mockcoordinator

build-coordinator output_dir:
    go build -o {{output_dir}}coordinator ./coordinatorservice/cmd/server

build-sigservice output_dir:
    go build -o {{output_dir}}sigservice ./sigverification/cmd/server

build-shardsservice output_dir:
    go build -o {{output_dir}}shardsservice ./shardsservice/cmd/server

build-result-gatherer output_dir:
    go build -o {{output_dir}}resultgatherer ./utils/experiment/cmd

### Deploy

#transfer-all +files=(default-deployment-files):
#    just list-hosts | while read line; do just deploy "$line" {{files}}; done

#transfer host +files=(default-deployment-files):
#    rsync -P -r {{files}} root@{{host}}:~

# Lists all hostnames from the inventory
list-hosts name=("all") property=("ansible_host"):
    ansible {{name}} --list-hosts | tail -n +2 | \
      while read line; do ansible-inventory --host $line | jq {{'.' + property}} | sed -e 's/^"//' -e 's/"$//';done

# The docker image required for compilation on the Unix machines
docker-image:
    docker build -f builder/Dockerfile -t sc_builder .

# Executes a command from within the docker image. Requires that docker-image be run once before, to ensure the image exists.
docker CMD:
    docker run --rm -it -v "$PWD":/scalable-committer --env GOPROXY=direct -w /scalable-committer sc_builder:latest {{CMD}}

docker-runner-dir := "runner/"
docker-runner-config-dir := docker-runner-dir + "config/"
docker-runner-bin-dir := docker-runner-dir + "bin/"

docker-runner-image inventory=('ansible/inventory/hosts-local-docker.yaml'):
    mkdir -p {{linux-bin-input-dir}}
    mkdir -p {{docker-runner-bin-dir}}
    rm {{docker-runner-bin-dir}}*
    mkdir -p {{docker-runner-config-dir}}
    rm {{docker-runner-config-dir}}*
    just docker "just build-all {{linux-bin-input-dir}}"
    cp {{linux-bin-input-dir}}* {{docker-runner-bin-dir}}
    ansible-playbook "{{playbook-path}}/20-create-service-base-config.yaml" -i {{inventory}} --extra-vars "{'src_dir': '{{'../../' + config-input-dir}}', 'dst_dir': '{{'../../' + base-setup-config-dir}}'}"
    cp {{base-setup-config-dir + '*'}} {{docker-runner-config-dir}}
    docker build -f runner/Dockerfile -t sc_runner .

docker-run-services:
    docker run --rm -dit -p 5002:5002 sc_runner:latest

docker-run-blockgen:
    GOGC=20000 ./eval/experiments/local-bin/blockgen stream --configs ./eval/experiments/local-configs/blockgen-machine-config-blockgen.yaml

deploy-base-setup:
    mkdir -p {{osx-bin-input-dir}}
    just build-all {{osx-bin-input-dir}}
    mkdir -p {{linux-bin-input-dir}}
    just docker "just build-all {{linux-bin-input-dir}}"
    just deploy-bins
    just deploy-base-configs

#
deploy-bins local_linux_src_dir=('../../' + linux-bin-input-dir) local_osx_src_dir=('../../' + osx-bin-input-dir):
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['blockgen'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'coordinators', 'filenames': ['coordinator', 'mockcoordinator'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sigservices', 'filenames': ['sigservice'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'shardsservices', 'filenames': ['shardsservice'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"

# Copies config/profile files from the local host to the corresponding remote servers
# Each server will receive only the files it needs
deploy-base-configs local_src_dir=('../../' + config-input-dir) local_dst_dir=('../../' + base-setup-config-dir):
    ansible-playbook "{{playbook-path}}/20-create-service-base-config.yaml" --extra-vars "{'src_dir': '{{local_src_dir}}', 'dst_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'coordinators', 'filenames': ['config-coordinator.yaml'], 'src_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'sigservices', 'filenames': ['config-sigservice.yaml'], 'src_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'shardsservices', 'filenames': ['config-shardsservice.yaml'], 'src_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['config-blockgen.yaml', 'profile-blockgen.yaml'], 'src_dir': '{{local_dst_dir}}'}"

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
    echo "sig_verifiers,shard_servers,large_txs,invalidity_ratio,double_spends,block_size,start_time,"{{sampling-time-header}} > "{{experiment-tracking-dir}}/{{experiment_name}}.csv"; \
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
    just gather-results "{{experiment_name}}.csv"

run-experiment sig_verifiers=("3") shard_servers=("3") large_txs=("0.0") invalidity_ratio=("0.0") double_spends=("0.0") block_size=("100") local_src_dir=('../../' + base-setup-config-dir):
    just start-servers {{sig_verifiers}} {{shard_servers}} {{local_src_dir}}
    just start-blockgen {{large_txs}} {{invalidity_ratio}} {{double_spends}} {{block_size}} {{local_src_dir}}

start-servers  sig_verifiers=("3") shard_servers=("3") local_src_dir=('../../' + base-setup-config-dir):
    ansible-playbook "{{playbook-path}}/50-create-coordinator-experiment-config.yaml" --extra-vars "{'src_dir': {{local_src_dir}}, 'sig_verifiers': {{sig_verifiers}}, 'shard_servers': {{shard_servers}}}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'coordinators', 'filenames': ['config-coordinator.yaml'], 'src_dir': '{{local_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"sig_verifiers": {{sig_verifiers}}, "shard_servers": {{shard_servers}}}'

start-blockgen large_txs=("0.0") invalidity_ratio=("0.0") double_spends=("0.0") block_size=("100") local_src_dir=('../../' + base-setup-config-dir):
    ansible-playbook "{{playbook-path}}/60-create-blockgen-experiment-config.yaml" --extra-vars "{'src_dir': {{local_src_dir}}, 'large_txs': {{large_txs}}, 'small_txs': $(bc <<< "1 - {{large_txs}}"), 'invalidity_ratio': {{invalidity_ratio}}, 'double_spends': {{double_spends}}, 'block_size': {{block_size}}}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['config-blockgen.yaml', 'profile-blockgen.yaml'], 'src_dir': '{{local_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/80-start-blockgen.yaml"

# Goes through all of the entries of the tracker file and retrieves the corresponding metric for each line (as defined at the sampling-time field)
gather-results filename:
    mkdir -p {{experiment-results-dir}}
    {{bin-input-dir}}resultgatherer -client-endpoint=$(just list-hosts blockgens):$(just list-hosts blockgens prometheus_exporter_port) -prometheus-endpoint=$(just list-hosts monitoring):{{prometheus-scraper-port}} -output={{experiment-results-dir}}{{filename}} -rate-interval=2m -input={{experiment-tracking-dir}}{{filename}} -sampling-time-header={{sampling-time-header}}

kill-all sig_verifiers=("100") shard_servers=("100"):
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"sig_verifiers": {{sig_verifiers}}, "shard_servers": {{shard_servers}}, "only_kill": true}'

# Unix and OSX have different expressions to retrieve the timestamp
get-timestamp plus_seconds=("0") format=(""):
    #date --date="+{{plus_seconds}} seconds" +%s #bash
    date -v +{{plus_seconds}}S {{format}} #osx
