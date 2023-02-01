#!/usr/bin/env just --justfile

### Constants

#default-deployment-files := "bin eval/deployments/default/*"

default := ''
project-dir := env_var_or_default('PWD', '.')
runner-dir := project-dir + "/runner/out"
config-input-dir := project-dir + "/config"

output-dir := project-dir + "/eval"


deployment-dir := output-dir + "/deployments"

# Stores the configs that are deployed to the servers
base-setup-config-dir := deployment-dir + "/configs"
# Stores the credentials for the communication with the orderers
base-setup-creds-dir := deployment-dir + "/creds"
# Stores the genesis block for the orderers
base-setup-genesis-dir := deployment-dir + "/genesis"
# Stores the binaries that are deployed to the servers
bin-input-dir := deployment-dir + "/bins"
osx-bin-input-dir := bin-input-dir + "/osx"
linux-bin-input-dir := bin-input-dir + "/linux"

experiment-dir := output-dir + "/experiments"
# Each file in this folder keeps track of an experiment suite.
# Each line contains the experiment parameters, when it started and when we should sample.
experiment-tracking-dir := experiment-dir + "/trackers"
# Each file in this folder contains the results for an experiment suite.
# Each line corresponds one-to-one to the lines of the respective track file with the same name.
experiment-results-dir := experiment-dir + "/results"

# Experiment constants
experiment-duration-seconds := "600"

fabric_path := env_var_or_default('FABRIC_PATH', env_var('GOPATH') + "/src/github.com/hyperledger/fabric")

# Well-known ports
prometheus-scraper-port := "9091"

playbook-path := "./ansible/playbooks"
export ANSIBLE_CONFIG := env_var_or_default('ANSIBLE_CONFIG', './ansible/ansible.cfg')

sampling-time-header := "sample_time"
array-separator := ","

default-channel-id := "mychannel"
all-instances := '100'

### Admin
update-dependencies:
    ansible-playbook "{{playbook-path}}/0-update-deps.yaml"

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

clean target_hosts=('all') include_configs=('false') include_bins=('false'):
    ansible-playbook "{{playbook-path}}/95-clean-all.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}', 'include_bins': '{{include_bins}}', 'include_configs': '{{include_configs}}'}"

deploy-base-setup include_bins=('false'):
    just clean all true {{include_bins}}
    if [[ "{{include_bins}}" = "true" ]]; then \
      just build-committer-bins; \
      just build-orderer-bins; \
      just deploy-committer-bins; \
      just deploy-orderer-bins; \
    fi

    just build-committer-configs
    just build-orderer-configs
    just deploy-committer-configs
    just deploy-orderer-configs

    just build-creds
    just deploy-creds
    just build-genesis-block
    just deploy-genesis-block

build-deploy-committer:
    just build-committer-bins
    just deploy-committer-bins
    just build-committer-configs
    just deploy-committer-configs

#########################
# Binaries
#########################

build-committer-bins local=('true') docker=('true'):
    if [[ "{{local}}" = "true" ]]; then \
      just empty-dir {{osx-bin-input-dir}}; \
      just build-committer-local {{osx-bin-input-dir}}; \
    fi

    if [[ "{{docker}}" = "true" ]]; then \
      just empty-dir {{linux-bin-input-dir}}; \
      just build-committer-docker {{linux-bin-input-dir}}; \
    fi

build-orderer-bins local=('true') docker=('true'):
    if [[ "{{local}}" = "true" ]]; then \
      mkdir -p {{osx-bin-input-dir}}; \
      just build-raft-orderers-local {{osx-bin-input-dir}}; \
      just build-orderer-clients-local {{osx-bin-input-dir}}; \
    fi
    if [[ "{{docker}}" = "true" ]]; then \
      mkdir -p {{linux-bin-input-dir}}; \
      just build-raft-orderers-docker {{linux-bin-input-dir}}; \
      just build-orderer-clients-docker {{linux-bin-input-dir}}; \
    fi

deploy-committer-bins:
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['blockgen'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'coordinators', 'filenames': ['coordinator', 'coordinator_setup', 'mockcoordinator'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sigservices', 'filenames': ['sigservice'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'shardsservices', 'filenames': ['shardsservice'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sidecars', 'filenames': ['sidecar'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sidecarclients', 'filenames': ['sidecarclient'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"

deploy-orderer-bins:
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'ordererlisteners', 'filenames': ['ordererlistener'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'orderersubmitters', 'filenames': ['orderersubmitter'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'orderingservices', 'filenames': ['orderer', 'mockorderingservice'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"

# builds the fabric binaries
# make sure you on the expected fabric version branch
# git checkout v2.4.7 -b v2.4.7-branch
build-raft-orderers-local output_dir:
    make -C {{fabric_path}} native
    if [ -d "{{output_dir}}" ]; then \
      cp -r "{{fabric_path}}/build/bin/" "{{output_dir}}"; \
    fi

build-raft-orderers-docker output_dir signed=('false'):
    docker run --rm -it -v {{output_dir}}:/usr/local/out orderer_builder:latest /usr/local/rebuild_binaries.sh {{signed}} /usr/local/out

build-committer-local output_dir:
    just build-blockgen {{output_dir}}
    just build-coordinator {{output_dir}}
    just build-sigservice {{output_dir}}
    just build-shardsservice {{output_dir}}
    just build-result-gatherer {{output_dir}}
    just build-sidecar {{output_dir}}
    just build-sidecar-client {{output_dir}}

build-orderer-clients-local output_dir:
    just build-mock-orderer {{output_dir}}
    just build-orderer-listener {{output_dir}}
    just build-orderer-submitter {{output_dir}}

build-committer-docker output_dir:
    just docker "just build-committer-local ./eval/deployments/bins/linux"

build-orderer-clients-docker output_dir:
    just docker "just build-orderer-clients-local ./eval/deployments/bins/linux"

build-blockgen output_dir:
    go build -o {{output_dir}}/blockgen ./wgclient/cmd/generator
    go build -o {{output_dir}}/mockcoordinator ./wgclient/cmd/mockcoordinator

build-coordinator output_dir:
    go build -o {{output_dir}}/coordinator ./coordinatorservice/cmd/server
    go build -o {{output_dir}}/coordinator_setup ./coordinatorservice/cmd/setup_helper

build-sigservice output_dir:
    go build -o {{output_dir}}/sigservice ./sigverification/cmd/server

build-shardsservice output_dir:
    go build -o {{output_dir}}/shardsservice ./shardsservice/cmd/server

build-result-gatherer output_dir:
    go build -o {{output_dir}}/resultgatherer ./utils/experiment/cmd

build-sidecar output_dir:
    go build -o {{output_dir}}/sidecar ./sidecar/cmd/server

build-mock-orderer output_dir:
    just build-ordering-main ./clients/cmd/mockorderer mockorderingservice {{output_dir}}

build-orderer-listener output_dir:
    just build-ordering-main ./clients/cmd/listener ordererlistener {{output_dir}}

build-orderer-submitter output_dir:
    just build-ordering-main ./clients/cmd/submitter orderersubmitter {{output_dir}}

build-ordering-main main_path output_name output_path:
    just empty-dir ./orderingservice/fabric/temp
    cd ./orderingservice/fabric; go build -o ./temp/{{output_name}} {{main_path}}; cd ../..
    cp ./orderingservice/fabric/temp/* {{output_path}}
    rm -r ./orderingservice/fabric/temp

build-sidecar-client output_dir:
    go build -o {{output_dir}}/sidecarclient ./wgclient/cmd/sidecarclient

docker-orderer-image:
    docker build -f ordererbuilder/Dockerfile -t orderer_builder .

#########################
# Credentials
#########################

build-creds:
    just empty-dir "{{base-setup-creds-dir}}/orgs"
    docker run --rm -it \
          -v {{base-setup-creds-dir}}:/usr/local/out \
          -v {{base-setup-config-dir}}/:/usr/local/crypto-config \
          orderer_builder:latest /usr/local/init.sh /usr/local/crypto-config/crypto-config.yaml "" /usr/local/out ""
    just copy-client-creds

build-genesis-block channel_id=(default-channel-id):
    docker run --rm -it \
          -v {{base-setup-creds-dir}}:/usr/local/out \
          -v {{base-setup-config-dir}}/:/usr/local/txgen-config \
          orderer_builder:latest /usr/local/init.sh "" /usr/local/txgen-config/configtx.yaml /usr/local/out {{channel_id}}

deploy-creds orderers=(all-instances):
    ansible-playbook "{{playbook-path}}/45-transfer-client-creds.yaml" --extra-vars "{'input_creds_path': '{{base-setup-creds-dir}}', 'input_config_path': '{{base-setup-config-dir}}'}"
    ansible-playbook "{{playbook-path}}/46-transfer-orderer-creds.yaml" --extra-vars "{'input_creds_path': '{{base-setup-creds-dir}}', 'instances': {{orderers}}}"

deploy-genesis-block orderers=(all-instances):
    ansible-playbook "{{playbook-path}}/47-transfer-orderer-genesis-block.yaml" --extra-vars "{'input_creds_path': '{{base-setup-creds-dir}}', 'instances': {{orderers}}}"

# Copies the necessary files (creds and configs) for the orderer listener and submitter. Only needed for listener/main.go and submitter/main.go.
copy-client-creds dst_path=(project-dir + '/orderingservice/fabric/out'):
    #!/usr/bin/env bash
    just empty-dir {{dst_path}}
    any_orderer_name=$(just list-hosts orderingservices .orderer_name | tail -n 1)
    cp {{base-setup-creds-dir}}/orgs/ordererOrganizations/orderer.org/orderers/$any_orderer_name.orderer.org/tls/ca.crt {{dst_path}}
    cp -r {{base-setup-creds-dir}}/orgs/peerOrganizations/org1.com/users/User1@org1.com/msp {{dst_path}}
    cp {{base-setup-config-dir}}/orderer.yaml {{dst_path}}

#########################
# Configs
#########################

build-committer-configs:
    ansible-playbook "{{playbook-path}}/20-create-service-base-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'channel_id': '{{default-channel-id}}'}"

# Copies config/profile files from the local host to the corresponding remote servers
# Each server will receive only the files it needs
deploy-committer-configs:
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'coordinators', 'src_dir': '{{base-setup-config-dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'sigservices', 'src_dir': '{{base-setup-config-dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'shardsservices', 'src_dir': '{{base-setup-config-dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'blockgens', 'src_dir': '{{base-setup-config-dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'sidecars', 'src_dir': '{{base-setup-config-dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'sidecarclients', 'src_dir': '{{base-setup-config-dir}}'}"

deploy-orderer-configs orderers=(all-instances):
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'orderingservices', 'src_dir': '{{base-setup-config-dir}}', 'instances': {{orderers}}}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'ordererlisteners', 'src_dir': '{{base-setup-config-dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'orderersubmitters', 'src_dir': '{{base-setup-config-dir}}'}"

# Create config files (configtx.yaml, orderer.yaml, crypto-config.yaml) based on the inventory
build-orderer-configs orderers=(all-instances) output_dir=(base-setup-config-dir) input_dir=('$PWD/config/testdata'):
    mkdir -p {{output_dir}}
    spruce merge {{input_dir}}/configtx.yaml >> {{output_dir}}/configtx.yaml
    spruce merge {{input_dir}}/orderer.yaml >> {{output_dir}}/orderer.yaml
    spruce merge {{input_dir}}/crypto-config.yaml >> {{output_dir}}/crypto-config.yaml
    ansible-playbook "{{playbook-path}}/94-create-orderer-config.yaml" --extra-vars "{'configtx_path': '{{output_dir}}/configtx.yaml', 'orderer_path': '{{output_dir}}/orderer.yaml', 'crypto_config_path': '{{output_dir}}/crypto-config.yaml', 'orderingservice_instances': {{orderers}}}"

list-host-names name=("all"):
    ansible {{name}} --list-hosts | tail -n +2

get-property host query:
    ansible-inventory --host {{host}} | jq '{{query}}' | sed -e 's/^"//' -e 's/"$//'

# Lists all hostnames from the inventory
list-hosts name query:
     just list-host-names {{name}} | while read line; do just get-property "$line" "{{query}}";done

# The docker image required for compilation on the Unix machines
docker-image:
    docker build -f builder/Dockerfile -t sc_builder .

# Executes a command from within the docker image. Requires that docker-image be run once before, to ensure the image exists.
docker CMD:
    docker run --rm -it -v "$PWD":/scalable-committer --env GOPROXY=direct -w /scalable-committer sc_builder:latest {{CMD}}

#########################
# Simple containerized SC
#########################

docker-runner-image:
    just build-committer-bins false
    just deploy-committer-bins
    docker build -f runner/Dockerfile -t sc_runner .

    just build-committer-configs
    just deploy-committer-configs

# creds_dir should contain: msp/, ca.crt, orderer.yaml
docker-run-services public_key_dir=(project-dir + '/coordinatorservice/cmd/setup_helper/testdata/') orderer_config_dir=(project-dir + '/eval/deployments/configs/'):
    docker run --rm -dit \
    -p 5050:5050 \
    -v {{runner-dir}}/config:/root/config \
    -v {{public_key_dir}}:/root/pubkey \
    -v {{ if orderer_config_dir != '' { orderer_config_dir } else { runner-dir + '/orderer-creds'} }}:/root/orderer-creds \
    sc_runner:latest

docker-build-blockgen inventory=("ansible/inventory/hosts-local-docker.yaml"):
    just build-blockgen {{osx-bin-input-dir}}
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" -i {{inventory}} --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['blockgen'], 'osx_src_dir': '{{osx-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" -i {{inventory}} --extra-vars "{'target_hosts': 'blockgens', 'src_dir': '{{base-setup-config-dir}}'}"

docker-run-blockgen:
    GOGC=20000 {{osx-bin-input-dir}}/blockgen stream --configs {{base-setup-config-dir}}blockgen-machine-config-blockgen.yaml


#########################
# Run
#########################

start-mock-coordinator local_src_dir=(base-setup-config-dir):
    ansible-playbook "{{playbook-path}}/50-create-coordinator-experiment-config.yaml" --extra-vars "{'src_dir': {{local_src_dir}}}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'coordinators', 'src_dir': '{{local_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["mockcoordinator"]}'

start-mock-orderers:
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["mockorderingservice"]}'

run-all-orderer-experiment-suites:
    just run-orderer-experiment-suite "all_orderer_experiments" "1,2,3,4" "1,2,3,4" "160,5000" "3,5,7,11"

# Creates binaries, configs, creds for the orderers.
# Before the first orderer experiment, run for all orderers, so that they all are set up with the right binaries and credentials.
# just deploy-orderer-experiment-setup 100 true true
# For subsequent runs with less orderers, it is not necessary to re-build the creds and bins, but we need to create new configs and genesis block. For instance:
# just deploy-orderer-experiment-setup 3; just run-orderer-experiment 1 1 160 3
# just deploy-orderer-experiment-setup 4; just run-orderer-experiment 1 1 160 4
# ...
# It is not necessary to re-build configs and genesis block between runs that have the same amount of orderers:
# just deploy-orderer-experiment-setup 4; just run-orderer-experiment 1 1 160 4; just run-orderer-experiment 2 4 5000 4
deploy-orderer-experiment-setup orderers=(all-instances) include_creds=('false') include_bins=('false'):
    just clean all {{include_creds}} {{include_bins}}
    if [[ "{{include_bins}}" = "true" ]]; then \
      just build-orderer-bins; \
      just deploy-orderer-bins; \
    fi
    just build-orderer-configs {{orderers}}
    just deploy-orderer-configs {{orderers}}
    if [[ "{{include_creds}}" = "true" ]]; then \
        just build-creds; \
        just deploy-creds {{orderers}}; \
    fi
    just build-genesis-block;
    just deploy-genesis-block {{orderers}}

# Runs a series of orderer experiments. Make sure you have initialized all orderers before:
# just deploy-orderer-experiment-setup 100 true true
# just run-orderer-experiment-suite my-experiment 1,2 1,2,3 160,5000 1,3,5
run-orderer-experiment-suite  experiment_name connections_arr=('1') streams_per_connection_arr=('1') message_size_arr=('160') orderers_arr=('3') experiment_duration=(experiment-duration-seconds):
    mkdir -p {{experiment-tracking-dir}}
    echo "connections,streams_per_connection,messages,message_size,orderers,start_time,"{{sampling-time-header}} > "{{experiment-tracking-dir}}/{{experiment_name}}.csv"; \
    echo {{orderers_arr}} | tr '{{array-separator}}' '\n' | while read orderers; do \
      just kill-all; \
      just deploy-orderer-experiment-setup $orderers; \
      echo {{streams_per_connection_arr}} | tr '{{array-separator}}' '\n' | while read streams_per_connection; do \
        echo {{message_size_arr}} | tr '{{array-separator}}' '\n' | while read message_size; do \
          echo {{connections_arr}} | tr '{{array-separator}}' '\n' | while read connections; do \
              echo "Running experiment {{experiment_name}} for {{experiment_duration}} seconds. Settings:\n\t$connections connections\n\t$streams_per_connection streams per connection\n\t$message_size B message size\n\t$orderers orderers\nExperiment records are stored in {{experiment-tracking-dir}}/{{experiment_name}}.csv.\n"; \
              just run-orderer-experiment $connections $streams_per_connection $message_size $orderers; \
              echo $connections,$streams_per_connection,$message_size,$orderers,$(just get-timestamp 0 +%s),$(just get-timestamp {{experiment_duration}} +%s) >> "{{experiment-tracking-dir}}/{{experiment_name}}.csv"; \
              echo "Waiting experiment {{experiment_name}} until $(just get-timestamp {{experiment_duration}}). Settings:\n\t$connections connections\n\t$streams_per_connection streams per connection\n\t$message_size B message size\n\t$orderers orderers\nExperiment records are stored in {{experiment-tracking-dir}}/{{experiment_name}}.csv.\n"; \
              sleep {{experiment_duration}} \
            ;done \
        ;done \
      ;done \
    done

# Runs an orderer experiment from scratch:
# - kills all existing orderer/listener/submitter instances
# - cleans all existing ledgers
# - starts all orderers, 1 listener, and 1 submitter.
# To restart the experiment, we can run the same command again. Make sure the machines have been properly initialized (same amount of orderers):
# just deploy-orderer-experiment-setup 100 true true
# just run-orderer-experiment 1 1 160 100
# just run-orderer-experiment 1 1 5000 100
# just deploy-orderer-experiment-setup 4
# just run-orderer-experiment 1 1 160 4
run-orderer-experiment connections=('1') streams_per_connection=('1') message_size=('160') orderers=(all-instances) channel_id=(default-channel-id) listeners=(all-instances) submitters=(all-instances):
    just kill-all
    just clean all
    just start-raft-orderers {{orderers}} "{{channel_id}}"
    just start-orderer-listeners {{listeners}} {{orderers}} "{{channel_id}}"
    just start-orderer-submitters {{submitters}} {{orderers}} {{connections}} {{streams_per_connection}} {{message_size}} "{{channel_id}}"

start-raft-orderers orderers=(all-instances) channel_id=(default-channel-id):
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["orderer"], "orderingservice_instances": {{orderers}}}'

start-orderer-listeners listeners=(all-instances) orderers=(all-instances) channel_id=(default-channel-id):
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["ordererlistener"], "ordererlistener_instances": {{listeners}}, "orderingservice_instances": {{orderers}}, "channel_id": "{{channel_id}}"}'

start-orderer-submitters submitters=(all-instances) orderers=(all-instances) connections=('1') streams_per_connection=('1') message_size=('160') channel_id=(default-channel-id):
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["orderersubmitter"], "orderersubmitter_instances": {{submitters}}, "orderingservice_instances": {{orderers}}, "connections": {{connections}}, "streams_per_connection": {{streams_per_connection}}, "message_size": {{message_size}}, "channel_id": "{{channel_id}}"}'

start-mir-orderers channel_id=(default-channel-id):
    #!/usr/bin/env bash
    cp config/testdata/configtx.yaml orderingservice/mirbft; \
    cp config/testdata/crypto-config.yaml orderingservice/mirbft; \
    cd orderingservice/fabric; just init {{channel_id}}; cd ../..; \
    rm -r orderingservice/mirbft/out; cp -R orderingservice/fabric/out orderingservice/mirbft/; \
    mkdir orderingservice/mirbft/out/creds; \
    cp orderingservice/fabric/out/orgs/ordererOrganizations/orderer.org/orderers/raft0.orderer.org/tls/server.crt orderingservice/mirbft/out/creds; \
    cp orderingservice/fabric/out/orgs/ordererOrganizations/orderer.org/orderers/raft0.orderer.org/tls/server.key orderingservice/mirbft/out/creds; \

    set -euxo pipefail
    i=0
    just list-host-names orderingservices | while read line; do \
      service_port=$(just get-property $line "(.service_port|tostring)"); \
      session_name=$line; \
      echo "Running orderer $i on port $service_port\n"; \
      cd ./orderingservice/mirbft; tmux new-session -s $session_name -d "just run_orderer_on_port $i $service_port"; cd ../..; \
      i=$((i+1)) \
    ; done

start-sidecar channel_id=(default-channel-id) local_src_dir=(base-setup-config-dir):
    ansible-playbook "{{playbook-path}}/91-create-sidecar-experiment-config.yaml" --extra-vars "{'src_dir': {{local_src_dir}}, 'channel_id': '{{channel_id}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'sidecars', 'src_dir': '{{local_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["sidecar"]}'

start-sidecar-clients channel_id=(default-channel-id) local_src_dir=(base-setup-config-dir):
    ansible-playbook "{{playbook-path}}/93-create-sidecar-client-experiment-config.yaml" --extra-vars "{'src_dir': {{local_src_dir}}, 'channel_id': '{{channel_id}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'sidecarclients', 'src_dir': '{{local_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["sidecarclient"]}'

start-all committer=('sc') orderer=('raft') channel_id=(default-channel-id) local_src_dir=(base-setup-config-dir):
    just clean all

    if [[ "{{committer}}" = "mock" ]]; then \
      just start-mock-coordinator {{local_src_dir}}; \
    elif [[ "{{committer}}" = "sc" ]]; then \
      just start-committer 100 100 {{local_src_dir}}; \
    else \
      echo "Committer type {{committer}} not defined"; \
      exit 1; \
    fi

    if [[ "{{orderer}}" = "mock" ]]; then \
      just start-mock-orderers; \
    elif [[ "{{orderer}}" = "raft" ]]; then \
      just start-raft-orderers {{channel_id}}; \
    elif [[ "{{orderer}}" = "mir" ]]; then \
      just start-mir-orderers {{channel_id}}; \
    else \
      echo "Orderer type {{orderer}} not defined"; \
      exit 1; \
    fi

    just start-sidecar {{channel_id}} {{local_src_dir}}
    just start-sidecar-clients {{channel_id}} {{local_src_dir}}

    tmux set-option remain-on-exit

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

run-experiment sig_verifiers=("3") shard_servers=("3") large_txs=("0.0") invalidity_ratio=("0.0") double_spends=("0.0") block_size=("100") local_src_dir=(base-setup-config-dir):
    just start-committer {{sig_verifiers}} {{shard_servers}} {{local_src_dir}}
    just start-blockgen {{large_txs}} {{invalidity_ratio}} {{double_spends}} {{block_size}} {{local_src_dir}}

start-committer  sig_verifiers=("3") shard_servers=("3") local_src_dir=(base-setup-config-dir):
    ansible-playbook "{{playbook-path}}/50-create-coordinator-experiment-config.yaml" --extra-vars "{'src_dir': {{local_src_dir}}, 'sig_verifiers': {{sig_verifiers}}, 'shard_servers': {{shard_servers}}}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'coordinators', 'src_dir': '{{local_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["sigservice", "shardsservice", "coordinator"], "sigservice_instances": {{sig_verifiers}}, "shardsservice_instances": {{shard_servers}}}'

start-blockgen large_txs=("0.0") invalidity_ratio=("0.0") double_spends=("0.0") block_size=("100") local_src_dir=(base-setup-config-dir):
    ansible-playbook "{{playbook-path}}/60-create-blockgen-experiment-config.yaml" --extra-vars "{'src_dir': {{local_src_dir}}, 'large_txs': {{large_txs}}, 'small_txs': $(bc <<< "1 - {{large_txs}}"), 'invalidity_ratio': {{invalidity_ratio}}, 'double_spends': {{double_spends}}, 'block_size': {{block_size}}}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'blockgens', 'src_dir': '{{local_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["blockgen stream"]}'

# Goes through all of the entries of the tracker file and retrieves the corresponding metric for each line (as defined at the sampling-time field)
gather-results filename:
    mkdir -p {{experiment-results-dir}}
    {{bin-input-dir}}resultgatherer -client-endpoint=$(just list-hosts blockgens "(.ansible_host) + \\\":\\\" + (.prometheus_exporter_port|tostring)") -prometheus-endpoint=$(just list-hosts monitoring "(.ansible_host) + \\\":{{prometheus-scraper-port}}\\\"") -output={{experiment-results-dir}}{{filename}} -rate-interval=2m -input={{experiment-tracking-dir}}{{filename}} -sampling-time-header={{sampling-time-header}}

restart-monitoring remove_existing=('true'):
    go run utils/monitoring/cmd/main.go -config-dir utils/monitoring/config/ -remove-existing {{remove_existing}}

kill-all:
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["sigservice", "shardsservice", "coordinator", "mockcoordinator", "blockgen stream", "mockorderingservice", "sidecar", "sidecarclient", "orderer", "ordererlistener", "orderersubmitter"], "only_kill": true}'

# Unix and OSX have different expressions to retrieve the timestamp
get-timestamp plus_seconds=("0") format=(""):
    #date --date="+{{plus_seconds}} seconds" +%s #bash
    date -v +{{plus_seconds}}S {{format}} #osx

empty-dir dir:
    if [ -d "{{dir}}" ]; then \
      rm -r "{{dir}}"; \
    fi
    mkdir -p {{dir}}