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

clean-all include_bins=('false') include_configs=('false') target_hosts=('all'):
    ansible-playbook "{{playbook-path}}/95-clean-all.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}', 'include_bins': '{{include_bins}}', 'include_configs': '{{include_configs}}'}"

deploy-base-setup include_bins=('false'):
    just clean-all {{include_bins}} true
    if [[ "{{include_bins}}" = "true" ]]; then \
      just build-bins; \
      just deploy-bins; \
    fi

    just build-base-configs
    just deploy-base-configs

    just build-creds
    just deploy-creds

#########################
# Binaries
#########################

build-bins:
    just empty-dir {{osx-bin-input-dir}}
    just build-committer-local {{osx-bin-input-dir}}
    just build-raft-orderers-local {{osx-bin-input-dir}}

    just empty-dir {{linux-bin-input-dir}}
    just build-committer-docker {{linux-bin-input-dir}}
    just build-raft-orderers-docker {{linux-bin-input-dir}}

deploy-bins local_linux_src_dir=(linux-bin-input-dir) local_osx_src_dir=(osx-bin-input-dir):
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['blockgen'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'coordinators', 'filenames': ['coordinator', 'mockcoordinator'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sigservices', 'filenames': ['sigservice'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'shardsservices', 'filenames': ['shardsservice'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'orderingservices', 'filenames': ['orderer', 'mockorderingservice'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sidecars', 'filenames': ['sidecar'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sidecarclients', 'filenames': ['sidecarclient'], 'osx_src_dir': '{{local_osx_src_dir}}', 'linux_src_dir': '{{local_linux_src_dir}}'}"

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
    just build-mock-orderer {{output_dir}}
    just build-sidecar-client {{output_dir}}

build-committer-docker output_dir:
    just docker "just build-committer-local ./eval/deployments/bins/linux"

build-blockgen output_dir:
    go build -o {{output_dir}}/blockgen ./wgclient/cmd/generator
    go build -o {{output_dir}}/mockcoordinator ./wgclient/cmd/mockcoordinator

build-coordinator output_dir:
    go build -o {{output_dir}}/coordinator ./coordinatorservice/cmd/server

build-sigservice output_dir:
    go build -o {{output_dir}}/sigservice ./sigverification/cmd/server

build-shardsservice output_dir:
    go build -o {{output_dir}}/shardsservice ./shardsservice/cmd/server

build-result-gatherer output_dir:
    go build -o {{output_dir}}/resultgatherer ./utils/experiment/cmd

build-sidecar output_dir:
    go build -o {{output_dir}}/sidecar ./sidecar/cmd/server

build-mock-orderer output_dir:
    just empty-dir ./orderingservice/fabric/temp
    cd ./orderingservice/fabric; go build -o ./temp/mockorderingservice ./clients/cmd/mockorderer; cd ../..
    cp ./orderingservice/fabric/temp/* {{output_dir}}
    rm -r ./orderingservice/fabric/temp

build-sidecar-client output_dir:
    go build -o {{output_dir}}/sidecarclient ./wgclient/cmd/sidecarclient

docker-orderer-image:
    docker build -f ordererbuilder/Dockerfile -t orderer_builder .

#########################
# Credentials
#########################

build-creds channel_id=(default-channel-id):
    just docker-init-orderer {{base-setup-creds-dir}} {{channel_id}} {{base-setup-config-dir}}/crypto-config.yaml {{base-setup-config-dir}}/configtx.yaml
    just copy-client-creds

deploy-creds:
    ansible-playbook "{{playbook-path}}/45-transfer-client-creds.yaml" --extra-vars "{'input_creds_path': '{{base-setup-creds-dir}}', 'input_config_path': '{{base-setup-config-dir}}'}"
    ansible-playbook "{{playbook-path}}/46-transfer-orderer-creds.yaml" --extra-vars "{'input_creds_path': '{{base-setup-creds-dir}}'}"

# Copies the necessary files (creds and configs) for the orderer listener and submitter. Only needed for listener/main.go and submitter/main.go.
copy-client-creds dst_path=(project-dir + '/orderingservice/fabric/out'):
    #!/usr/bin/env bash
    just empty-dir {{dst_path}}
    any_orderer_name=$(just list-hosts orderingservices .orderer_name | tail -n 1)
    cp {{base-setup-creds-dir}}/orgs/ordererOrganizations/orderer.org/orderers/$any_orderer_name.orderer.org/tls/ca.crt {{dst_path}}
    cp -r {{base-setup-creds-dir}}/orgs/peerOrganizations/org1.com/users/User1@org1.com/msp {{dst_path}}
    cp {{base-setup-config-dir}}/orderer.yaml {{dst_path}}

docker-init-orderer out_dir channel_id=(default-channel-id) crypto_config=('$PWD/config/testdata/crypto-config.yaml') txgen_config=('$PWD/config/testdata/configtx.yaml'):
    echo "Clean up {{out_dir}}"
    just empty-dir {{out_dir}}

    echo "Generate crypto and configtx in {{out_dir}}"
    docker run --rm -it \
      -v {{out_dir}}:/usr/local/out \
      -v $(dirname "{{crypto_config}}"):/usr/local/crypto-config \
      -v $(dirname "{{txgen_config}}"):/usr/local/txgen-config \
      orderer_builder:latest /usr/local/init.sh /usr/local/crypto-config/$(basename "{{crypto_config}}") /usr/local/txgen-config/$(basename "{{txgen_config}}") /usr/local/out {{channel_id}}

#########################
# Configs
#########################

build-base-configs local_src_dir=(config-input-dir) local_dst_dir=(base-setup-config-dir):
    just empty-dir {{local_dst_dir}}
    ansible-playbook "{{playbook-path}}/20-create-service-base-config.yaml" --extra-vars "{'src_dir': '{{local_src_dir}}', 'dst_dir': '{{local_dst_dir}}', 'channel_id': '{{default-channel-id}}'}"
    just create-orderer-configs {{local_dst_dir}}

# Copies config/profile files from the local host to the corresponding remote servers
# Each server will receive only the files it needs
deploy-base-configs local_dst_dir=(base-setup-config-dir):
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'coordinators', 'src_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'sigservices', 'src_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'shardsservices', 'src_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'blockgens', 'src_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'sidecars', 'src_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'sidecarclients', 'src_dir': '{{local_dst_dir}}'}"
    ansible-playbook "{{playbook-path}}/30-transfer-base-config.yaml" --extra-vars "{'target_hosts': 'orderingservices', 'src_dir': '{{local_dst_dir}}'}"

# Create config files (configtx.yaml, orderer.yaml, crypto-config.yaml) based on the inventory
create-orderer-configs output_dir input_dir=('$PWD/config/testdata'):
    spruce merge {{input_dir}}/configtx.yaml >> {{output_dir}}/configtx.yaml
    spruce merge {{input_dir}}/orderer.yaml >> {{output_dir}}/orderer.yaml
    spruce merge {{input_dir}}/crypto-config.yaml >> {{output_dir}}/crypto-config.yaml
    ansible-playbook "{{playbook-path}}/94-create-orderer-config.yaml" --extra-vars "{'configtx_path': '{{output_dir}}/configtx.yaml', 'orderer_path': '{{output_dir}}/orderer.yaml', 'crypto_config_path': '{{output_dir}}/crypto-config.yaml'}"

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

docker-runner-image include_bins=('true'):
    just deploy-base-setup {{include_bins}}

    docker build -f runner/Dockerfile -t sc_runner .

# creds_dir should contain: msp/, ca.crt, orderer.yaml
docker-run-services orderer_config_dir=(''):
    docker run --rm -dit \
    -p 5002:5002 \
    -p 5050:5050 \
    -v {{runner-dir}}/config:/root/config \
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

start-raft-orderers channel_id=(default-channel-id):
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["orderer"]}'

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
    just clean-all false false

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

kill-all:
    ansible-playbook "{{playbook-path}}/70-start-hosts.yaml" --extra-vars '{"start": ["sigservice", "shardsservice", "coordinator", "mockcoordinator", "blockgen stream", "mockorderingservice", "sidecar", "sidecarclient", "orderer"], "only_kill": true}'

# Unix and OSX have different expressions to retrieve the timestamp
get-timestamp plus_seconds=("0") format=(""):
    #date --date="+{{plus_seconds}} seconds" +%s #bash
    date -v +{{plus_seconds}}S {{format}} #osx

empty-dir dir:
    if [ -d "{{dir}}" ]; then \
      rm -r "{{dir}}"; \
    fi
    mkdir -p {{dir}}