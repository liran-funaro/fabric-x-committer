#!/usr/bin/env just --justfile

### Constants

#default-deployment-files := "bin eval/deployments/default/*"

default := ''
project-dir := env_var_or_default('PWD', '.')
docker-project-dir := project-dir
runner-dir := project-dir + "/runner/out"
config-input-dir := project-dir + "/config"

# File containing the passwords for all FSC nodes
password-file-path := env_var_or_default('SC_FSC_PASSWORD_FILE', config-input-dir + "/passwords.yml")

orderer-builder-dir := project-dir + "/ordererbuilder"
sc-builder-dir := project-dir + "/builder"

output-dir := project-dir + "/eval"


deployment-dir := output-dir + "/deployments"

# Stores the configs that are deployed to the servers
base-setup-config-dir := deployment-dir + "/configs"
# Stores the config files for the monitoring components
monitoring-setup-config-dir := base-setup-config-dir + "/monitoring"
# Stores the static UI info to be served for the web and mobile applications
ui-setup-config-dir := base-setup-config-dir + "/ui-configs"
# Stores the credentials for the communication with the orderers
base-setup-creds-dir := deployment-dir + "/creds"
base-setup-orderer-artifacts-dir := deployment-dir + "/orderer-artifacts"
# Stores the genesis block for the orderers
base-setup-genesis-dir := deployment-dir + "/genesis"
# Stores the binaries that are deployed to the servers
bin-input-dir := deployment-dir + "/bins"
local-bin-input-dir := bin-input-dir + "/local"
linux-bin-input-dir := bin-input-dir + "/linux"

experiment-dir := output-dir + "/experiments"
# Each file in this folder keeps track of an experiment suite.
# Each line contains the experiment parameters, when it started and when we should sample.
experiment-tracking-dir := experiment-dir + "/trackers"
# Each file in this folder contains the results for an experiment suite.
# Each line corresponds one-to-one to the lines of the respective track file with the same name.
experiment-results-dir := experiment-dir + "/results"

topology-setup-dir := project-dir + "/topologysetup"
default-generated-main-path := topology-setup-dir + "/tmp"

monitoring-config-dir := project-dir + "/utils/monitoring/config"

# Experiment constants
experiment-duration-seconds := "1200"

fabric-path := env_var_or_default('FABRIC_PATH', env_var('GOPATH') + "/src/github.com/hyperledger/fabric")
fabric-bins-path := fabric-path + "/build/bin"

# Well-known ports
prometheus-scraper-port := "9091"
grafana-ui-port := '3001'
jaeger-ui-port := '16686'
jaeger-collector-port := '14268'

github-user := env_var_or_default('SC_GITHUB_USER', '')
github-token := env_var_or_default('SC_GITHUB_TOKEN', '')

playbook-path := "./ansible/playbooks"
export ANSIBLE_CONFIG := env_var_or_default('ANSIBLE_CONFIG', './ansible/ansible.cfg')

sampling-time-header := "sample_time"
array-separator := ","

default-channel-id := "mychannel"
default-topology-name := "mytopos"
all-instances := '100'

N_A := '0'

signed-envelopes := 'false'
boosted-orderer := 'false'

# Use HTTPS for the REST endpoints of the FSC nodes
tls-fsc-rest-api := 'false'

#########################
# Quickstart
#########################

build:
	./scripts/build_all.sh

test:
    go test -v ./...

bootstrap:
    just docker
    git clone https://github.com/hyperledger/fabric.git {{fabric-path}}

docker:
    just docker-orderer-image
    just docker-builder-image

launch target_hosts orderer=('raft') committer=('sc'):
    just run {{target_hosts}} false false false

run target_hosts=('all') join_channel=('true') init_chaincode=('true') init_committer_key=('true') ui_config=('true'):
    #!/usr/bin/env bash

    # Start orderer
    ansible-playbook "{{playbook-path}}/71-start-orderer.yaml" --extra-vars "{'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}'}"
    sleep 15

    if [[ "{{join_channel}}" = "true" ]]; then \
      just orderer-admin join-channel; \
    fi

    # Start Fabric peers
    ansible-playbook "{{playbook-path}}/70-start-peer.yaml" --extra-vars "{'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}'}"

    if [[ "{{join_channel}}" = "true" ]]; then \
      just peer-admin join-channel; \
    fi
    if [[ "{{init_chaincode}}" = "true" ]]; then \
      just peer-admin install-chaincode; \
      just peer-admin approve-chaincode-for-org; \
      just peer-admin commit-chaincode; \
      just peer-admin invoke-chaincode; \
    fi

    # Start committer
    ansible-playbook "{{playbook-path}}/61-start-sigverifier.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/62-start-shardsservice.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/63-start-coordinator.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}', 'action': 'start'}"

    # Start sidecar
    ansible-playbook "{{playbook-path}}/65-start-sidecar.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"

    if [[ "{{init_committer_key}}" = "true" ]]; then \
      # Set committer key from endorser crypto material
      just set-committer-key; \
    fi

    # Start fsc nodes
    ansible-playbook "{{playbook-path}}/72-start-fscservices.yaml" --extra-vars "{'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'tls': {{tls-fsc-rest-api}}, 'mode': 'bootstrap_only'}"
    ansible-playbook "{{playbook-path}}/72-start-fscservices.yaml" --extra-vars "{'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'tls': {{tls-fsc-rest-api}}, 'mode': 'exclude_bootstrap'}"

    # Start blockgen
    ansible-playbook "{{playbook-path}}/64-start-blockgen.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"
    # Start sidecar clients
    ansible-playbook "{{playbook-path}}/66-start-sidecarclient.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"
    # Start orderer listeners
    ansible-playbook "{{playbook-path}}/67-start-ordererlistener.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"
    # Start orderer submitters
    ansible-playbook "{{playbook-path}}/68-start-orderersubmitter.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"

    if [[ "{{ui_config}}" = "true" ]]; then \
      just serve-ui-configs; \
    fi

setup orderer_type=('etcdraft') local_bins=('false') docker_bins=('false'):
    #!/usr/bin/env bash
    just kill
    just clean all true {{ if local_bins == 'true' { 'true' } else if docker_bins == 'true' { 'true' } else { 'false' } }}

    just build-committer-bins {{local_bins}} {{docker_bins}}
    just build-orderer-bins {{local_bins}} {{docker_bins}}; \
    just build-configs {{orderer_type}}
    just build-orderer-artifacts {{orderer_type}}
    just build-fsc-bins {{local_bins}} {{docker_bins}}
    just build-ui-configs

    just deploy-configs
    if [[ "{{local_bins}}" = "true" || "{{docker_bins}}" = "true" ]]; then \
      just deploy-bins; \
    fi

check-ports:
    ansible-playbook "{{playbook-path}}/91-check-ports.yaml"

clean target_hosts=('all') include_configs=('false') include_bins=('false'):
    ansible-playbook "{{playbook-path}}/95-clean.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}', 'include_bins': {{include_bins}}, 'include_configs': {{include_configs}}, 'topology_name': '{{default-topology-name}}'}"

call-api target_host user action recipient=('') value=('0') nonce=(''):
    ansible-playbook "{{playbook-path}}/75-call-api.yaml" --extra-vars "{'target_host': '{{target_host}}', 'user': '{{user}}', 'action': '{{action}}', 'value': {{value}}, 'recipient': '{{recipient}}', 'nonce': '{{nonce}}', 'tls': {{tls-fsc-rest-api}}, 'password_file': '{{password-file-path}}'}"

limit-rate limit=('-1'):
    ansible-playbook "{{playbook-path}}/76-limit-rate.yaml" --extra-vars "{'limit': {{limit}}}"

kill target_hosts=('all'):
    ansible-playbook "{{playbook-path}}/90-kill.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"

ping target_hosts=('all'):
    ansible {{target_hosts}} -m ping -v

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

build-committer-bins local=('true') docker=('true'):
    #!/usr/bin/env bash
    if [[ "{{local}}" = "true" ]]; then \
      just build-committer-local {{local-bin-input-dir}}; \
    fi

    if [[ "{{docker}}" = "true" ]]; then \
      just docker-builder-run "just build-committer-local {{docker-project-dir}}/eval/deployments/bins/linux"; \
    fi

build-orderer-bins local=('true') docker=('true'):
    #!/usr/bin/env bash
    if [[ "{{local}}" = "true" ]]; then \
      just build-orderers-local; \
      just build-orderer-clients-local {{local-bin-input-dir}}; \
    fi

    if [[ "{{docker}}" = "true" ]]; then \
      just build-orderers-docker; \
      just docker-builder-run "just build-orderer-clients-local {{docker-project-dir}}/eval/deployments/bins/linux"; \
    fi

build-fsc-bins local=('true') docker=('true'):
    #!/usr/bin/env bash
    if [[ "{{local}}" = "true" ]]; then \
      just build-fsc-bins-local {{local-bin-input-dir}}; \
    fi
    if [[ "{{docker}}" = "true" ]]; then \
      just docker-builder-run "just build-fsc-bins-local {{docker-project-dir}}/eval/deployments/bins/linux"; \
    fi

build-fsc-bins-local output_dir generated_main_path=(default-generated-main-path):
    cd {{generated_main_path}}/cmd; for d in */ ; do go build -buildvcs=false -o "{{output_dir}}/${d%/*}" "$d/main.go"; done

# builds the fabric binaries
# make sure you on the expected fabric version branch
# git checkout v2.4.7 -b v2.4.7-branch
build-orderers-local:
    #!/usr/bin/env bash
    cd "{{fabric-path}}" || exit; \
    git reset --hard; \
    make clean; \

    echo "Applying patch to fix RequestMaxBytes config property..."; \
    git apply {{orderer-builder-dir}}/fix_RequestMaxBytes.patch; \
    echo "Applying patch to accept MESSAGE type..."; \
    git apply {{orderer-builder-dir}}/allow_MESSAGE_type.patch; \
    if [[ "{{signed-envelopes}}" = "true" ]]; then \
      echo "Building orderer binaries for signed envelopes..."; \
    else \
      echo "Applying patch and building orderer binaries for unsigned envelopes..."; \
      git apply {{orderer-builder-dir}}/orderer_no_sig_check.patch; \
    fi; \
    if [[ "{{boosted-orderer}}" = "true" ]]; then \
      echo "Applying booster patch and building orderer binaries..."; \
      git apply {{orderer-builder-dir}}/orderer_booster.patch; \
    else \
      echo "Building orderer binaries without booster patch..."; \
    fi; \
    git status; \
    make -C {{fabric-path}} native; \
    echo "Bins created under {{fabric-bins-path}}. Copying to {{local-bin-input-dir}}..."; \
    cp -a "{{fabric-bins-path}}/." "{{local-bin-input-dir}}/"

build-orderers-docker:
    just docker-orderer-run "/usr/local/build_orderer_binaries.sh {{signed-envelopes}} {{boosted-orderer}} {{docker-project-dir}}/eval/deployments/bins/linux/"

build-committer-local output_dir:
    go build -buildvcs=false -o {{output_dir}}/blockgen ./wgclient/cmd/generator
    go build -buildvcs=false -o {{output_dir}}/mockcoordinator ./wgclient/cmd/mockcoordinator
    go build -buildvcs=false -o {{output_dir}}/coordinator ./coordinatorservice/cmd/server
    go build -buildvcs=false -o {{output_dir}}/coordinator_setup ./coordinatorservice/cmd/setup_helper
    go build -buildvcs=false -o {{output_dir}}/sigservice ./sigverification/cmd/server
    go build -buildvcs=false -o {{output_dir}}/shardsservice ./shardsservice/cmd/server
    go build -buildvcs=false -o {{output_dir}}/resultgatherer ./utils/experiment/cmd
    go build -buildvcs=false -o {{output_dir}}/sidecar ./sidecar/cmd/server
    go build -buildvcs=false -o {{output_dir}}/sidecarclient ./wgclient/cmd/sidecarclient

build-orderer-clients-local output_dir:
    just build-ordering-main ./clients/cmd/mockorderer mockorderingservice {{output_dir}}
    just build-ordering-main ./clients/cmd/listener ordererlistener {{output_dir}}
    just build-ordering-main ./clients/cmd/submitter orderersubmitter {{output_dir}}

build-ordering-main main_path output_name output_path:
    just empty-dir ./orderingservice/fabric/temp
    cd ./orderingservice/fabric; go build -buildvcs=false -o ./temp/{{output_name}} {{main_path}}; cd ../..
    cp ./orderingservice/fabric/temp/* {{output_path}}
    rm -r ./orderingservice/fabric/temp

deploy-bins:
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'blockgens', 'filenames': ['blockgen'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'coordinators', 'filenames': ['coordinator', 'coordinator_setup', 'mockcoordinator'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sigservices', 'filenames': ['sigservice'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'shardsservices', 'filenames': ['shardsservice'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sidecars', 'filenames': ['sidecar'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'sidecarclients', 'filenames': ['sidecarclient'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'peerservices', 'filenames': ['peer'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'ordererlisteners', 'filenames': ['ordererlistener'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'orderersubmitters', 'filenames': ['orderersubmitter'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/31-transfer-fsc-bin.yaml" --extra-vars "{'target_hosts': 'all', 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': 'orderingservices', 'filenames': ['orderer', 'mockorderingservice', 'osnadmin'], 'osx_src_dir': '{{local-bin-input-dir}}', 'linux_src_dir': '{{linux-bin-input-dir}}'}"

#TODO: Remove workaround once we start building the orderers from the source code
replace-orderer-bins orderer_bin_path:
    just replace-bins orderer orderingservices {{orderer_bin_path}}
replace-issuer-bins issuer_bin_path:
    just replace-bins issuer issuer {{issuer_bin_path}}
replace-bins bin target_hosts bin_path:
    cp {{bin_path}} {{linux-bin-input-dir}}/{{bin}}
    ansible-playbook "{{playbook-path}}/40-transfer-service-bin.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}', 'filenames': ['{{bin}}'], 'linux_src_dir': '{{linux-bin-input-dir}}', 'osx_src_dir': ''}"

# Executes a command from within the docker image. Requires that docker-builder-image be run once before, to ensure the image exists.
docker-builder-run CMD:
    just docker-image-run sc_builder "{{CMD}}"

docker-orderer-run CMD:
    just docker-image-run orderer_builder "{{CMD}}"

docker-image-run docker_image CMD:
    #!/usr/bin/env bash
    if [[ "{{github-user}}" = "" || "{{github-token}}" = "" ]]; then \
      echo "Building without private github repo"; \
      docker run --rm -it -v "{{project-dir}}":{{docker-project-dir}} -w {{docker-project-dir}} orderer_builder:latest {{CMD}}; \
    else \
      echo "Building with private github repo. User: {{github-user}}"; \
      docker run --rm -it -v "{{project-dir}}":{{docker-project-dir}} --env GOPRIVATE=github.ibm.com/* -w {{docker-project-dir}} {{docker_image}}:latest sh -c "git config --global url.\"https://{{github-user}}:{{github-token}}@github.ibm.com/\".insteadOf https://github.ibm.com/; {{CMD}}"; \
    fi

# The docker image required for compilation of the orderer and the related bins
docker-orderer-image:
    just docker-image-build {{orderer-builder-dir}} orderer_builder

# The docker image required for compilation of SC components on the Unix machines
docker-builder-image:
    just docker-image-build {{sc-builder-dir}} sc_builder

docker-image-build dockerfile_dir docker_image:
    docker build -f {{dockerfile_dir}}/Dockerfile -t {{docker_image}} .

# Simple containerized SC
docker-runner-image:
    just build-committer-bins true false
    mkdir -p {{runner-dir}}/bin
    cp {{linux-bin-input-dir}}/* {{runner-dir}}/bin
    docker build -f runner/Dockerfile -t sc_runner .

#########################
# Configs, Credentials, and Genesis
#########################

build-configs orderer_type target_hosts=('all'):
    ansible-playbook "{{playbook-path}}/21-create-sigverifier-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/22-create-shardsservice-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/23-create-coordinator-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/24-create-blockgen-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/25-create-sidecar-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'channel_id': '{{default-channel-id}}', 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/26-create-sidecarclient-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'channel_id': '{{default-channel-id}}', 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'orderer_type': '{{orderer_type}}', 'signed_envelopes': {{signed-envelopes}}}"
    ansible-playbook "{{playbook-path}}/27-create-ordererlistener-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'channel_id': '{{default-channel-id}}', 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/28-create-orderersubmitter-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'channel_id': '{{default-channel-id}}', 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'orderer_type': '{{orderer_type}}', 'signed_envelopes': {{signed-envelopes}}}"
    ansible-playbook "{{playbook-path}}/32-create-fsc-config.yaml" --extra-vars "{'orderer_artifacts_path': '{{base-setup-orderer-artifacts-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'target_hosts': '{{target_hosts}}'}"

build-ui-configs:
    ansible-playbook "{{playbook-path}}/30-create-ui-config.yaml" --extra-vars "{'dst_dir': '{{ui-setup-config-dir}}', 'tls': {{tls-fsc-rest-api}}}"

serve-ui-configs port=('8080'):
    #!/usr/bin/env bash
    docker stop static-ui-config
    docker rm static-ui-config
    echo "Serving UI configs under http://localhost:{{port}}/ui-config.yaml, http://localhost:{{port}}/ui-config.json"
    docker run -w /app -p {{port}}:8080 -v {{config-input-dir}}:/etc/nginx -v {{ui-setup-config-dir}}:/app/static --name static-ui-config nginx:alpine &

build-orderer-artifacts orderer_type=('BFT') local_fab_bins=('false') topology_config_path=(base-setup-config-dir + '/topology-setup-config.yaml'):
    #!/usr/bin/env bash
    ansible-playbook "{{playbook-path}}/29-create-topology-setup-config.yaml" --extra-vars "{'dst_dir': '{{base-setup-config-dir}}', 'topology_name': '{{default-topology-name}}', 'orderer_type': '{{orderer_type}}', 'channel_ids': ['{{default-channel-id}}']}"

    if [[ -f "{{topology_config_path}}" ]]; then \
      echo "Creating NWO artifacts based on topology setup in {{topology_config_path}}..."; \
      just empty-dir {{base-setup-orderer-artifacts-dir}}; \

      if [[ "{{local_fab_bins}}" = "true" ]]; then \
        export GOPRIVATE=github.ibm.com/*; \
        just build-orderer-artifacts-local {{project-dir}} {{local-bin-input-dir}}; \
      else \
        just docker-orderer-run "just build-orderer-artifacts-local {{docker-project-dir}} {{docker-project-dir}}/eval/deployments/bins/linux"; \
      fi; \
    else \
      echo "No Fabric topology setup found. Skipping..."; \
    fi

build-orderer-artifacts-local root_dir fab_bin_dir:
    cd {{root_dir}}/topologysetup; \
    go get github.com/IBM/idemix/tools/idemixgen@v0.0.2-0.20230403120754-d7dbe0340c4a; \
    go run {{root_dir}}/topologysetup/generatetopology/cmd/generate/main.go --configs {{root_dir}}/eval/deployments/configs/topology-setup-config.yaml --out-dir {{root_dir}}/topologysetup/tmp/; \
    cd ./tmp; \
    go run ./main.go --output-dir {{root_dir}}/eval/deployments/orderer-artifacts --fab-bin-dir {{fab_bin_dir}} --configs {{root_dir}}/eval/deployments/configs/topology-setup-config.yaml

# Copies config/profile files from the local host to the corresponding remote servers
# Each server will receive only the files it needs
deploy-configs target_hosts=('all') include_configs=('true') include_creds=('true') include_genesis=('true') include_chaincode=('true'):
    ansible-playbook "{{playbook-path}}/41-transfer-sigverifier-config.yaml" --extra-vars "{'input_dir': '{{base-setup-config-dir}}', 'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/42-transfer-shardsservice-config.yaml" --extra-vars "{'input_dir': '{{base-setup-config-dir}}', 'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/43-transfer-coordinator-config-creds.yaml" --extra-vars "{'input_dir': '{{base-setup-config-dir}}', 'target_hosts': '{{target_hosts}}', 'include_creds': {{include_creds}}, 'topology_name': '{{default-topology-name}}', 'orderer_artifacts_path': '{{base-setup-orderer-artifacts-dir}}'}"
    ansible-playbook "{{playbook-path}}/44-transfer-blockgen-config.yaml" --extra-vars "{'input_dir': '{{base-setup-config-dir}}', 'target_hosts': '{{target_hosts}}'}"
    ansible-playbook "{{playbook-path}}/45-transfer-sidecar-config-creds.yaml" --extra-vars "{'input_dir': '{{base-setup-config-dir}}', 'orderer_artifacts_path': '{{base-setup-orderer-artifacts-dir}}', 'include_creds': {{include_creds}}, 'include_configs': {{include_configs}}, 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'current_config_dir': '{{base-setup-orderer-artifacts-dir}}'}"
    ansible-playbook "{{playbook-path}}/46-transfer-sidecarclient-config-creds.yaml" --extra-vars "{'input_dir': '{{base-setup-config-dir}}', 'orderer_artifacts_path': '{{base-setup-orderer-artifacts-dir}}', 'include_creds': {{include_creds}}, 'include_configs': {{include_configs}}, 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'current_config_dir': '{{base-setup-orderer-artifacts-dir}}'}"
    ansible-playbook "{{playbook-path}}/47-transfer-ordererlistener-config-creds.yaml" --extra-vars "{'input_dir': '{{base-setup-config-dir}}', 'orderer_artifacts_path': '{{base-setup-orderer-artifacts-dir}}', 'include_creds': {{include_creds}}, 'include_configs': {{include_configs}}, 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'current_config_dir': '{{base-setup-orderer-artifacts-dir}}'}"
    ansible-playbook "{{playbook-path}}/48-transfer-orderersubmitter-config-creds.yaml" --extra-vars "{'input_dir': '{{base-setup-config-dir}}', 'orderer_artifacts_path': '{{base-setup-orderer-artifacts-dir}}', 'include_creds': {{include_creds}}, 'include_configs': {{include_configs}}, 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'current_config_dir': '{{base-setup-orderer-artifacts-dir}}'}"
    ansible-playbook "{{playbook-path}}/49-transfer-peer-admin-config-creds.yaml" --extra-vars "{'orderer_artifacts_path': '{{base-setup-orderer-artifacts-dir}}', 'include_creds': {{include_creds}}, 'include_genesis': {{include_genesis}}, 'include_configs': {{include_configs}}, 'include_chaincode': {{include_chaincode}}, 'channel_ids': ['{{default-channel-id}}'], 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'current_config_dir': '{{base-setup-orderer-artifacts-dir}}'}"
    ansible-playbook "{{playbook-path}}/51-transfer-orderer-config-creds-genesis.yaml" --extra-vars "{'orderer_artifacts_path': '{{base-setup-orderer-artifacts-dir}}', 'include_creds': {{include_creds}}, 'include_genesis': {{include_genesis}}, 'include_configs': {{include_configs}}, 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'current_config_dir': '{{base-setup-orderer-artifacts-dir}}'}"
    ansible-playbook "{{playbook-path}}/52-transfer-fsc-config-creds.yaml" --extra-vars "{'input_dir': '{{base-setup-config-dir}}', 'orderer_artifacts_path': '{{base-setup-orderer-artifacts-dir}}', 'include_creds': {{include_creds}}, 'include_configs': {{include_configs}}, 'topology_name': '{{default-topology-name}}', 'target_hosts': '{{target_hosts}}', 'current_config_dir': '{{base-setup-orderer-artifacts-dir}}', 'password_file': '{{password-file-path}}'}"

#########################
# Run
#########################

set-committer-key:
    ansible-playbook "{{playbook-path}}/63-start-coordinator.yaml" --extra-vars "{'action': 'set-key', 'topology_name': '{{default-topology-name}}', 'target_hosts': 'all'}"

orderer-admin action:
    ansible-playbook "{{playbook-path}}/73-start-orderer-admin.yaml" --extra-vars "{'action': '{{action}}', 'channel_id': '{{default-channel-id}}', 'topology_name': '{{default-topology-name}}', 'target_hosts': 'all'}"

peer-admin action chaincode_name=('token-chaincode'):
    ansible-playbook "{{playbook-path}}/69-start-peer-admin.yaml" --extra-vars "{'action': '{{action}}', 'channel_id': '{{default-channel-id}}', 'chaincode_name': '{{chaincode_name}}', 'topology_name': '{{default-topology-name}}', 'target_hosts': 'peerservices[0]'}"

start-mir-orderers:
    #!/usr/bin/env bash
    cp config/testdata/configtx.yaml orderingservice/mirbft; \
    cp config/testdata/crypto-config.yaml orderingservice/mirbft; \
    cd orderingservice/fabric; just init {{default-channel-id}}; cd ../..; \
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

#########################
# Experiments
#########################

run-all-orderer-experiment-suites:
    just run-orderer-experiment-suite "all_orderer_experiments" "1,2,3,4" "1,2,3,4" "160,5000"

# Runs a series of orderer experiments. Make sure you have initialized all orderers every time you change the inventory:
# just build-deploy-all
# just run-orderer-experiment-suite my-experiment 1,2 1,2,3 160,5000
run-orderer-experiment-suite  experiment_name connections_arr=('1') streams_per_connection_arr=('1') message_size_arr=('160') signed=('true') experiment_duration=(experiment-duration-seconds):
    mkdir -p {{experiment-tracking-dir}}
    echo "connections,streams_per_connection,messages,message_size,start_time,"{{sampling-time-header}} > "{{experiment-tracking-dir}}/{{experiment_name}}.csv"; \
    echo {{streams_per_connection_arr}} | tr '{{array-separator}}' '\n' | while read streams_per_connection; do \
      echo {{message_size_arr}} | tr '{{array-separator}}' '\n' | while read message_size; do \
        echo {{connections_arr}} | tr '{{array-separator}}' '\n' | while read connections; do \
            echo "Running experiment {{experiment_name}} for {{experiment_duration}} seconds. Settings:\n\t$connections connections\n\t$streams_per_connection streams per connection\n\t$message_size B message size\nExperiment records are stored in {{experiment-tracking-dir}}/{{experiment_name}}.csv.\n"; \
            just run-orderer-experiment $connections $streams_per_connection $message_size {{signed}}; \
            echo $connections,$streams_per_connection,$message_size,$(date) >> "{{experiment-tracking-dir}}/{{experiment_name}}.csv"; \
            echo "Waiting experiment {{experiment_name}} from $(date). Settings:\n\t$connections connections\n\t$streams_per_connection streams per connection\n\t$message_size B message size\nExperiment records are stored in {{experiment-tracking-dir}}/{{experiment_name}}.csv.\n"; \
            sleep {{experiment_duration}} \
          ;done \
      ;done \
    done

run-orderer-experiment connections=('1') streams_per_connection=('1') message_size=('160') signed=('true'):
    just clean orderersubmitters true
    ansible-playbook "{{playbook-path}}/28-create-orderersubmitter-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'channel_id': '{{default-channel-id}}', 'topology_name': '{{default-topology-name}}', 'connections': {{connections}}, 'streams_per_connection': {{streams_per_connection}}, 'message_size': {{message_size}}, 'channel_id': '{{default-channel-id}}', 'signed': '{{signed}}', 'target_hosts': 'all'}"
    just deploy-configs orderersubmitters

    just run orderingservices false false false
    just launch ordererlisteners
    just launch orderersubmitters

run-variable-sigverifier-experiment:
    just run-experiment-suite "variable_sigverifiers" "1,2,3,4"
run-variable-shard-experiment:
    just run-experiment-suite "variable_shards" "3" "1,2,3,4,5,6"
run-variable-tx-sizes-experiment:
    just run-experiment-suite "variable_tx_sizes" "3" "3" "0.0,0.05,0.1,0.15,0.2,0.25,0.3"
run-variable-validity-ratio-experiment:
    just run-experiment-suite "variable_validity_ratios" "3" "3" '0.0' "0.0,0.05,0.1,0.15,0.2,0.25,0.3"
run-variable-double-spends-experiment:
    just run-experiment-suite "variable_double_spends" "3" "3" '0.0' '0.0' "0.0,0.05,0.1,0.15,0.2,0.25,0.3"
run-variable-block-size-experiment:
    just run-experiment-suite "variable_block_sizes" "3" "3" '0.0' "1.0" "50,100,3500,7000,15000,30000"

run-experiment-suite  experiment_name sig_verifiers_arr=('3') shard_servers_arr=('3') large_txs_arr=('0.0') invalidity_ratio_arr=('0.0') double_spends_arr=('0.0') block_sizes_arr=('100') experiment_duration=(experiment-duration-seconds):
    mkdir -p {{experiment-tracking-dir}}
    echo "sig_verifiers,shard_servers,large_txs,invalidity_ratio,double_spends,block_size,start_time,"{{sampling-time-header}} > "{{experiment-tracking-dir}}/{{experiment_name}}.csv"; \
    echo {{sig_verifiers_arr}} | tr '{{array-separator}}' '\n' | while read sig_verifiers; do \
      echo {{shard_servers_arr}} | tr '{{array-separator}}' '\n' | while read shard_servers; do \
        echo {{large_txs_arr}} | tr '{{array-separator}}' '\n' | while read large_txs; do \
          echo {{invalidity_ratio_arr}} | tr '{{array-separator}}' '\n' | while read invalidity_ratio; do \
            echo {{double_spends_arr}} | tr '{{array-separator}}' '\n' | while read double_spends; do \
                echo {{block_sizes_arr}} | tr '{{array-separator}}' '\n' | while read block_size; do \
                    echo "Running experiment {{experiment_name}} for {{experiment_duration}} seconds. Settings:\n\t$sig_verifiers Sig verifiers\n\t$shard_servers Shard servers\n\t$large_txs/1 Large TXs\n\t$invalidity_ratio/1 Invalidity ratio\n\t$double_spends/1 Double spends\n\t$block_size TXs per block\nExperiment records are stored in {{experiment-tracking-dir}}/{{experiment_name}}.csv.\n"; \
                    just run-committer-experiment $sig_verifiers $shard_servers $large_txs $invalidity_ratio $double_spends $block_size; \
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

run-committer-experiment sig_verifiers=('3') shards_services=('3') large_txs=('0.0') invalidity_ratio=('0.0') double_spends=('0.0') block_size=('100'):
    just clean coordinators,blockgens true
    ansible-playbook "{{playbook-path}}/23-create-coordinator-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'sig_verifiers': {{sig_verifiers}}, 'shard_servers': {{shards_services}}, 'dst_dir': '{{base-setup-config-dir}}', 'target_hosts': 'all'}"
    ansible-playbook "{{playbook-path}}/24-create-blockgen-config.yaml" --extra-vars "{'src_dir': '{{config-input-dir}}', 'dst_dir': '{{base-setup-config-dir}}', 'large_txs': {{large_txs}}, 'small_txs': $(bc <<< "1 - {{large_txs}}"), 'invalidity_ratio': {{invalidity_ratio}}, 'double_spends': {{double_spends}}, 'block_size': {{block_size}}, 'target_hosts': 'all'}"
    just deploy-configs coordinators,blockgens

    just launch sigservices[0:{{sig_verifiers}}]
    just launch shardsservices[0:{{shards_services}}]
    just launch coordinators
    just launch blockgens

# Goes through all of the entries of the tracker file and retrieves the corresponding metric for each line (as defined at the sampling-time field)
gather-results filename:
    mkdir -p {{experiment-results-dir}}
    {{bin-input-dir}}resultgatherer -client-endpoint=$(just list-hosts blockgens "(.ansible_host) + \\\":\\\" + (.prometheus_exporter_port|tostring)") -prometheus-endpoint=$(just list-hosts monitoring "(.ansible_host) + \\\":{{prometheus-scraper-port}}\\\"") -output={{experiment-results-dir}}{{filename}} -rate-interval=2m -input={{experiment-tracking-dir}}{{filename}} -sampling-time-header={{sampling-time-header}}

#########################
# Admin
#########################

update-dependencies target_hosts=('all'):
    ansible-playbook "{{playbook-path}}/0-update-deps.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"

update-passwords target_hosts=('all'):
    ansible-playbook "{{playbook-path}}/01-update-passwords.yaml" --extra-vars "{'target_hosts': '{{target_hosts}}'}"

#########################
# Utils
#########################

list-host-names name=('all'):
    ansible {{name}} --list-hosts | tail -n +2

get-property host query:
    ansible-inventory --host {{host}} | jq '{{query}}' | sed -e 's/^"//' -e 's/"$//'

# Lists all hostnames from the inventory
list-hosts name query:
     just list-host-names {{name}} | while read line; do just get-property "$line" "{{query}}";done

restart-monitoring tls_ca_dir=(base-setup-orderer-artifacts-dir + '/fsc/crypto/peerOrganizations/fsc.example.com/tlsca') prometheus_creds_path=(project-dir + '/prometheus-creds') grafana_creds_path=(project-dir + '/grafana-creds'):
    # Prometheus
    echo "Create prometheus.yml"
    ansible-playbook "{{playbook-path}}/33-create-prometheus-config.yaml" --extra-vars "{'dst_dir': '{{monitoring-setup-config-dir}}'}"

    echo "Creating Prometheus-client TLS credentials for FSC nodes"
    just empty-dir {{prometheus_creds_path}}
    openssl req \
      -newkey rsa:2048 \
      -nodes \
      -keyout {{prometheus_creds_path}}/fsc_node_client.key \
      -out {{prometheus_creds_path}}/fsc_node_client.csr
    openssl x509 \
      -req \
      -in {{prometheus_creds_path}}/fsc_node_client.csr \
      -out {{prometheus_creds_path}}/fsc_node_client.crt \
      -CA {{tls_ca_dir}}/tlsca.fsc.example.com-cert.pem \
      -CAkey {{tls_ca_dir}}/priv_sk \
      -set_serial 01 \
      -days 365 \
      -sha256
    chmod 444 {{prometheus_creds_path}}/fsc_node_client.key {{prometheus_creds_path}}/fsc_node_client.crt

    echo "Launching Prometheus"
    just docker-remove-container {{prometheus-scraper-port}}
    docker pull prom/prometheus:latest
    docker run --rm -dit \
        -p {{prometheus-scraper-port}}:9090 \
        -v {{monitoring-setup-config-dir}}/prometheus.yml:/etc/prometheus/prometheus.yml \
        -v /etc/hosts:/etc/hosts \
        -v {{prometheus_creds_path}}/:/var/tls_ca/ \
        --name my-prometheus-instance \
        prom/prometheus:latest
    echo "Prometheus running on http://localhost:{{prometheus-scraper-port}}"

    # Jaeger
    echo "Launching Jaeger"
    just docker-remove-container {{jaeger-ui-port}}
    docker pull jaegertracing/all-in-one:1.22
    docker run --rm -dit \
        -p {{jaeger-ui-port}}:16686 \
        -p {{jaeger-collector-port}}:14268 \
        -p 14269:14269 \
        -e JAEGER_SAMPLER_TYPE="const" \
        -e JAEGER_SAMPLER_PARAM="1" \
        jaegertracing/all-in-one:1.22
    echo "Jaeger running on http://localhost:{{jaeger-ui-port}}"

    # Grafana
    echo "Creating Grafana credentials under {{grafana_creds_path}}"
    just empty-dir {{grafana_creds_path}}
    openssl genrsa -out {{grafana_creds_path}}/grafana.key 2048
    openssl req -new -key {{grafana_creds_path}}/grafana.key -out {{grafana_creds_path}}/grafana.csr
    openssl x509 -req -days 365 -in {{grafana_creds_path}}/grafana.csr -signkey {{grafana_creds_path}}/grafana.key -out {{grafana_creds_path}}/grafana.crt
    chmod 444 {{grafana_creds_path}}/grafana.key {{grafana_creds_path}}/grafana.crt

    echo "Launching Grafana"
    just docker-remove-container {{grafana-ui-port}}
    docker pull grafana/grafana:latest
    docker run --rm -dit \
        -p {{grafana-ui-port}}:3000 \
        -e GF_AUTH_PROXY_ENABLED="true" \
        -e GF_PATHS_PROVISIONING="/etc/grafana/provisioning/" \
        -e _GF_PROMETHEUS_ENDPOINT="http://prometheus_instance:9090" \
        --link my-prometheus-instance:prometheus_instance \
        -v {{monitoring-config-dir}}/grafana-datasources.yml:/etc/grafana/provisioning/datasources/datasource.yml \
        -v {{monitoring-config-dir}}/grafana-dashboards.yml:/etc/grafana/provisioning/dashboards/dashboard.yml \
        -v {{monitoring-config-dir}}/prometheus-dashboard.json:/etc/grafana/provisioning/dashboards/prometheus-dashboard.json \
        -v {{monitoring-config-dir}}/node-exporter-dashboard.json:/etc/grafana/provisioning/dashboards/node-exporter-dashboard.json \
        -v {{monitoring-config-dir}}/grafana.ini:/etc/grafana/grafana.ini \
        -v {{grafana_creds_path}}/grafana.key:/etc/grafana/grafana.key \
        -v {{grafana_creds_path}}/grafana.crt:/etc/grafana/grafana.crt \
        grafana/grafana:latest
    echo "Grafana running on https://localhost:{{grafana-ui-port}}"

docker-remove-container port:
    #!/usr/bin/env bash

    for id in $(docker ps -q); do
        if [[ $(docker port "${id}") == *"{{port}}"* ]]; then
            echo "Stopping and removing container ${id}."
            docker stop "${id}"
            docker rm "${id}"
            exit 0
        fi
    done
    echo "No container found on port {{port}}."

get-timestamp plus_seconds=('0') format=(''):
    #!/usr/bin/env bash
    date --date="+{{plus_seconds}} seconds" +%s

empty-dir dir:
    if [ -d "{{dir}}" ]; then \
      rm -r "{{dir}}"; \
    fi
    mkdir -p {{dir}}
