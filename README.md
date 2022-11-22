# scalable-committer

## Quickstart

### Run on localhost
#### Install prerequisites
* Just (further details [here](https://github.com/casey/just#installation))
```shell
brew install just
```
* go 1.18
```shell
brew install go@1.18
```
* rocksdb
```shell
brew install rocksdb
```

#### Build and run
* Build binaries
```shell
just build-all
```
* Optional: Monitoring (For further details see section Monitoring > Setup)
* Run services
```shell
./bin/sigservice --configs ./config/config-sigservice.yaml,./config/config-sigservice-local.yaml
./bin/shardsservice --configs ./config/config-shardsservice.yaml,./config/config-shardsservice-local.yaml
./bin/coordinator --configs ./config/config-coordinator.yaml,./config/config-coordinator-local.yaml
./bin/blockgen stream --configs ./config/config-blockgen.yaml,./config/config-blockgen-local.yaml
```

### Run on remote hosts
#### Install prerequisites (on Mac)
This is when we build the code on our local machine and then send the binaries to the remote hosts for execution. This is easier for debugging, but it requires our localhost be on during the experiment runs.

* Golang 1.18 (see above)
* Just (see above)
* Docker engine (see above)
* Clone project
```shell
mkdir -p ~/go/src/github.com/decentralized-trust-search/scalable-committer
git clone https://github.ibm.com/decentralized-trust-research/scalable-committer.git ~/go/src/github.com/decentralized-trust-search/scalable-committer/
cd ~/go/src/github.com/decentralized-trust-search/scalable-committer/
```
* Docker image
```shell
just docker-image
```
* Ansible with its requirements (further details [here](./ansible/README.md))
```shell
brew install ansible
ansible-galaxy install -r ./ansible/requirements.yml
```
* Add SSH keys to known hosts (generate with `ssh-keygen` if none existing. Choose a name like `id_rsa`, so that it is picked by default)
```shell
ssh-keygen -t ed25519 -C "my_name"
ssh-copy-id -i ~/.ssh/deploy_key.pub root@tokentestbed1.sl.cloud9.ibm.com
...
ssh-copy-id -i ~/.ssh/deploy_key.pub root@tokentestbed15.sl.cloud9.ibm.com
```
* Optional: Monitoring (For further details see section Monitoring > Setup > Remote)

#### Install prerequisites (on Ubuntu)
This is when we want to build the code on a remote host and then send the binaries from there to the service hosts.
* Golang 1.18

```shell
apt-get update
wget https://go.dev/dl/go1.18.8.linux-amd64.tar.gz
tar xvf ./go1.18.8.linux-amd64.tar.gz
rm ~/go1.18.8.linux-amd64.tar.gz
mv ./go/ /usr/local/
export GOPATH=$HOME/go
export PATH=$PATH:/usr/local/go/bin:$GOPATH/bin
# go version
```
* Just
```shell
curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/just
export PATH=$PATH:/usr/local/just
# just —version
```
* Docker client (details [here](https://docs.docker.com/engine/install/ubuntu/))
* Clone project (same as on Mac)
* Docker image (same as on Mac)
* Ansible with its requirements
```shell
apt-install ansible
ansible-galaxy install -r requirements.yaml
```
* Add SSH keys to known hosts (same as on Mac)
* JQ
```shell
apt install jq
```

#### Build and run (with Ansible)

* Deploy base setup on the hosts as described in `hosts.yaml` (Make sure your certificate is added on the hosts, or you added the right password in `hosts.yaml`):
```shell
just deploy-base-setup
```
* Run experiment or experiment suite, resp:
```shell
just run-experiment
just run-experiment-suite "my_exp_suite"
```

#### Build and run (without Ansible)
This is the same process we follow as we do with Ansible, but done manually, for the sake of better understanding and possible troubleshooting.
* Build binaries
```shell
just docker "just build-all"
```
* Modify your configs, depending on the experiment setup and transfer binaries and configs
```shell
# Repeat for each signature verifier
scp ./bin/sigservice root@tokentestbed3.sl.cloud9.ibm.com:~/bin/
scp ./config/config-sigservice.yaml root@tokentestbed3.sl.cloud9.ibm.com:~/

# Repeat for each shards service
scp ./bin/shardsservice root@tokentestbed6.sl.cloud9.ibm.com:~/bin/
scp ./config/config-shardsservice.yaml root@tokentestbed6.sl.cloud9.ibm.com:~/

# Coordinator
scp ./bin/coordinator root@tokentestbed2.sl.cloud9.ibm.com:~/bin/
scp ./config/config-coordinator.yaml root@tokentestbed2.sl.cloud9.ibm.com:~/

# Blockgen
scp ./bin/blockgen root@tokentestbed1.sl.cloud9.ibm.com:~/bin/
scp ./config/config-blockgen.yaml root@tokentestbed1.sl.cloud9.ibm.com:~/
scp ./config/profile-blockgen.yaml root@tokentestbed1.sl.cloud9.ibm.com:~/
```
* Run services
```shell
# Repeat for each signature verifier
ssh root@tokentestbed3.sl.cloud9.ibm.com
tmux kill-session
tmux
./bin/sigservice --configs ./config-sigservice.yaml

# Repeat for each shards service
ssh root@tokentestbed6.sl.cloud9.ibm.com
tmux kill-session
tmux
./bin/shardsservice --configs ./config-shardsservice.yaml

# Coordinator
ssh root@tokentestbed2.sl.cloud9.ibm.com
tmux kill-session
tmux
./bin/coordinator --configs ./config-coordinator.yaml

# Blockgen
ssh root@tokentestbed1.sl.cloud9.ibm.com
tmux kill-session
tmux
./bin/blockgen --configs ./config-blockgen.yaml
```

## Background
The lifecycle of a transaction consists of 3 main stages:
* **Execution**: First the transaction is sent to an **endorser** that will execute the transaction based on its current view of the ledger (this view may be stale). Then the endorser signs the transaction and forwards it to the next stage.
* **Ordering**: The **orderer** will receive in parallel the signed transactions from the endorsers and will send them in a specific order to the next stage.
* **Validation**: It takes place at the committer and it checks whether:
  * the signature is valid (not corrupt and it belongs to the endorsers)
  * the tokens (inputs or **Serial Numbers/SN**) have not already spent in a previous transaction (using the order as defined by the orderer)

## Components

The scalable committer aims to provide a scalable solution of the validation phase and the two sub-tasks it consists of (validation and double-spend check). It consists of the following 3 types of components:
* **Signature verifiers**: One or more hosts that perform (in parallel) the validation check
* **Shard servers**: One or more hosts that perform (in parallel) the double-spend check. Each shard server is responsible for a specific range of SNs (based on the first 2 bytes).
* **Coordinator**: One host that performs the following operations:
  * Receives the transactions (in blocks) at the input of the committer (from the orderer)
  * Sends the transactions to the signature verifiers for parallel verification. The transactions are randomly sent to any of the available signature verifiers.
  * Analyzes the dependencies between different transactions that try to spend the same SN.
  * If a transaction only tries to use SNs that are not used by any previous valid transaction, and if this transaction has a valid signature, it sends it to the shard servers for the double-spend check. Contrary to what we saw in the case of the signature verifiers, the transaction is not sent randomly to any available shard server. Instead, given a block of transactions, we extract the contained SNs, we group them by shard server (each server is responsible for a specific range of SNs) and then we send one request to each shard server. Before resolving a transaction, we need to wait either for at least one negative response (double spend) or for all pending positive responses. 
    * If a transaction has an invalid signature, it will be rejected without a double-spend check.
    * If two transactions arrive at the same time, then the absolute order (as defined by the orderer) will be taken, the first one will be sent for a double-spend check. The second one will wait. If the former is valid, the latter will be rejected. Otherwise, the former gets rejected and the latter is sent for the double-spend check to the shard server.
  * Sends the result to the output. We have the following possible results:
    * **Valid**: The transaction is properly signed by the endorsers and does not try to spend any SNs that have been already spent.
    * **Invalid signature**: The transaction is not properly signed and hence a double-spend check is not even performed.
    * **Double spend**: The transaction has a valid signature, but one or more of the SNs have already been spent.

For the sake of the experiments, we have also the following 2 types of components:

* **Block generator**: One host that replaces the orderer in an experimental setup and creates the traffic (blocks of transactions) for the performance evaluation of the scalable committer as a blackbox.
* **Monitoring**: One host that communicates with all aforementioned hosts and runs the following services:
  * Jaeger exporter (collector)
  * Jaeger UI
  * Prometheus scraper
  * Grafana UI
For further details on these components see Monitoring.


## Monitoring

### Statistic types
Each host keeps track of two different kinds of statistics during execution:
* **Metrics**: Includes (among others) counters (e.g. how many requests), gauges (e.g. current size of a structure), histograms (e.g. duration of a request)
* **Traces**: Keeps records of uniquely-identified requests (in our case transactions) to create a timeline and analyze latency (when a request/transaction started, when it passed some checkpoints in the code/events, when it ended)

### Exporters

The application needs then to export these statistics. Exporting could be something simple as logging it on the console or something more complex like storing it in a database. For the purposes of our evaluation, the latter solution is adequate, so we will find two exporters in our application:
* **Prometheus exporter for metrics**: There is a go implementation for a go exporter, so instead of running a standalone server for this exporter, we start it by default together with the application. Then the application sends the metrics to a local port. To query the data in the prometheus exporter: `http://url.to.host:2112/metrics`, where `url.to.host` is the IP/hostname of the host that runs the exporter (in our case each host, e.g. signature verifier, block generator runs their own exporter) and `2112` the default port.
* **Jaeger exporter for traces**: Since there is no go implementation (to the best of our knowledge), we run a standalone docker instance on the monitoring host. Then the application sends the traces to a port on the remote host (organized in batches to reduce the communication overhead). To access the data of the Jaeger exporter: `http://url.to.monitoring:14269/metrics`, where `url.to.monitoring` is the IP/hostname of the monitoring host (in our case `tokentestbed16.sl.cloud9.ibm.com`) and `14269` the default port of the collector.

### Presentation

* For the **metrics** stored in the Prometheus exporter, we have two stages:
  * A Prometheus scraper (running on the monitoring host as a docker instance) scrapes the exporter every 15s. This scraper can be then queried by RESTful endpoints or its UI.
    * To access the UI of the scraper: `http://url.to.monitoring:9091`, where `9091` the port we chose (9090 is the default normally)
    * To use its RESTful API to issue queries: `http://url.to.monitoring:9091/api/v1/query?query=sc_e2e_responses`, where `sc_e2e_responses` is an example of a metric (or a query)
  * A Grafana instance then issues requests to the prometheus scraper and presents the results in graphs with several enhanced capabilities. To access the Grafana UI: `http://url.to.monitoring:3001`, where `3001` the port we picked (by default it is `3000`)
* For the **traces** stored in the Jaeger exporter, a Jaeger UI running as a docker instance queries the exporter and presents the data in a user-friendly way. To access its UI: `http://url.to.monitoring:16686/search`, where `16686` the default port

### Setup

* Install Docker engine on the monitoring host (i.e. for local setup: the localhost, for remote setup: the remote host responsible for monitoring, as defined in `hosts.yaml`).
* Optional: Install node exporter that runs by default on port 9100 on the service hosts (i.e. for local setup: the localhost, for remote setup: the blockgen, coordinator, sigservice, shardsservice hosts, as defined in `hosts.yaml`).
```shell
brew install node_exporter
```
* Adapt `./utils/monitoring/config/prometheus.yml` to point to the prometheus exporters of the configurations (field `prometheus.endpoint`), as well as the node-exporter default endpoint (9100).

  * For local setup:
  ```yaml
  scrape_configs:
    - job_name: go
      static_configs:
        - targets: [':9100', ':2113', ':2114', ':2115', ':2116']
  ```
  * For remote setup:
  ```yaml
  scrape_configs:
    - job_name: go
      static_configs:
        - targets: [
          'tokentestbed1:9100', 'tokentestbed1:2112',
          ...
          'tokentestbed14:9100', 'tokentestbed14:2112'
        ]
  ```
* Create and start docker instances (make sure the docker engine is up and running). You can change the default ports (as defined above) by setting the input flags. You can run this directly if you have go installed, otherwise you can create a binary and run it.

  * For local setup
  ```shell
  # Using directly go
  go run ./utils/monitoring/cmd/main.go -config-dir ./utils/monitoring/config
  
  # Building and running the binary
  go build -o ./bin/monitoring ./utils/monitoring/cmd/main.go
  ./bin/monitoring -config-dir ./utils/monitoring/config
  ```

  * For remote setup
  ```shell
  # Create binary so we don't need to install go
  just docker "go build -o ./bin/monitoring ./utils/monitoring/cmd/main.go"
  
  # Transfer files to monitoring-host, e.g. tokentestbed16.sl.cloud9.ibm.com
  scp ./bin/monitoring root@monitoring-host:~/bin/
  scp -r ./utils/monitoring/config/ root@monitoring-host:~/
  
  # Log in on monitoring host
  ./bin/monitoring -config-dir ./config
  ```

#### Remote

* Install prerequisites on the monitoring remote host (`monitoring` host as defined in `hosts.yaml`):
  * go 1.18
  * Docker engine
* Optional: Install node exporter on all service hosts for extra metrics.

## Experiments
For the experiment setup and execution, we use ansible, so before you proceed, make sure you read [this](./ansible/README.md).

### Base-setup deployment (remote)
Before executing any experiment, we have a "base setup", that is, the default values that we deploy to all servers (as defined in `hosts.yaml`).

The deployment of the base setup consists in the following:
* **Build binaries**: One binary is built for each service, and we store it under `./bin`. As we target Linux servers, all our SC components must be compiled for Linux.
  However, since most of us are using a Mac, compiling the shards server using `GOOS=linux go build` is not enough (e.g. rocksdb dependency).
  For that reason, we provide a docker container that can be used to build the SC component binaries.
* **Deploy binaries**: Each binary is sent to the corresponding host that needs it. On the remote host, they are stored under `~/bin`.
* **Deploy base configs**: All required base configs (under `/config`) are sent to the corresponding hosts that need them. On the remote host they are stored under `~`

The first time we deploy, we create the docker image that will build the binaries. This is a one-time operation:

```shell
just docker-image
```

After that, the three deployment tasks that we described above are performed with the following command:

```shell
just deploy-base-setup
```


### Single execution
The aim of the experiments is to measure the performance of the committer under different configurations. We have the following variable parameters for our experiments:
* Topology related
  * Number of shards
  * Number of signature verifiers
* Load related (for more options on load configuration, read [this](./wgclient/README.md))
  * Transaction size
  * Signature-validity ratio
  * Double-spend ratio
  * Block size

The configuration for the topology and the load, as described above, are included in the `config-coordinator.yaml` and `config-blockgen` accordingly. Hence, each experiment has consists of two steps:
* Modify the configuration (Create a copy of the local configuration, modify it, and sync it with the remote hosts)
* Run the servers (Run the hosts in the right order, i.e. first shards/signature verifiers, then coordinator and lastly block generator).
To run an experiment:
```shell
just run-experiment sig_verifiers=("3") shard_servers=("3") large_txs=("0.0") invalidity_ratio=("0.0") double_spends=("0.0") block_size=("100")
```
If no parameters are included, the base configuration parameters are taken.

**Important**: The monitoring service is not included in the experiment setup, because its a one-time operation.

### Experiment suite

Since we are interested in series of experiments we need to run the procedure above multiple times repetitively. Otherwise, we can run the command:
```shell
just run-experiment-suite  experiment_name sig_verifiers_arr=("3") shard_servers_arr=("3") large_txs_arr=("0.0") invalidity_ratio_arr=("0.0") double_spends_arr=("0.0") block_sizes_arr=("100") experiment_duration=(experiment-duration-seconds)
```

This command accepts comma-separated parameters (arrays) and will:
* Create a file `./eval/experiments/trackers/experiment_name.csv`, where the first time contains the headers and each subsequent row corresponds to one experiment run
* Iterate over the Cartesian product of all arrays passed as input (in our experiments we only use one parameter as array and the rest only has one value)
  * Add a row with the experiment parameters, the start time and the sampling time
  * Run an experiment with the given parameters for the specified time
* Read through all rows at the end of the experiment suite and query the monitoring server in order to collect the required metrics. The result will be stored under `./eval/experiments/results/experiment_name.csv`

We have the following default experiment suites that we will use in our evaluation:
```shell
just run-variable-sigverifier-experiment
just run-variable-shard-experiment
just run-variable-tx-sizes-experiment
just run-variable-validity-ratio-experiment
just run-variable-double-spends-experiment
just run-variable-block-size-experiment
```
