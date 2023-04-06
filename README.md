# Prerequisites

## Mac
1. Just (further details [here](https://github.com/casey/just#installation))
```shell
brew install just
```
2. Golang 1.20
```shell
brew install go@1.20
```
3. rocksdb and gorocksdb (*not required if you never build for the local machine, because the Docker image contains the dependency*)

First, we have to install rocksdb 7.9.2
```shell
wget https://raw.githubusercontent.com/Homebrew/homebrew-core/d23af025f1956dff0a6afade287192b61915cf09/Formula/rocksdb.rb
```
```shell 
brew install --build-from-source ./rocksdb.rb
```
Second, install gorocksdb
```shell
go get -u github.com/linxGnu/grocksdb@v1.7.15
```
Finally, make dynamic linker to pick the shared library
```shell
update_dyld_shared_cache
```

4. Ansible (for more info [here](https://docs.ansible.com/ansible/latest/installation_guide/intro_installation.html#pip-install)), and all the dependencies required by the project
```shell
brew install ansible
ansible-galaxy install -r requirements.yaml
```
5. Docker client
6. Required docker images
```shell
just bootstrap
```
7. tmux
```shell
brew install tmux
```
8. *Optional:* `jq` (only for some functionalities):
```shell
brew install jq
```
9. *Optional:* Monitoring
```shell
just restart-monitoring
```

## Linux
1. Just
```shell
curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/just
export PATH=$PATH:/usr/local/just
# just —version
```
2. Golang 1.20
```shell
apt-get update
wget https://go.dev/dl/go1.20.3.linux-amd64.tar.gz
tar xvf ./go1.20.3.linux-amd64.tar.gz
rm ./go1.20.3.linux-amd64.tar.gz
mv ./go/ /usr/local/
export GOPATH=$HOME/go
export PATH=$PATH:/usr/local/go/bin:$GOPATH/bin
# go version
```
3. rocksdb (*not required if you use this machine only for deployments, because the Docker image contains the dependency*)
```shell
apt-get update && apt-get install -y \
      libgflags-dev \
      libsnappy-dev \
      zlib1g-dev \
      libbz2-dev \
      libzstd-dev \
      liblz4-dev

cd /tmp && git clone https://github.com/facebook/rocksdb.git
cd /tmp/rocksdb && git checkout 7555243bcfb7086e8bad38d43a518ff4c53dc17a && DEBUG_LEVEL=0 make shared_lib && make install-shared
ldconfig
```
4. Ansible with its requirements
```shell
apt-install ansible
ansible-galaxy install -r requirements.yaml
```
5. Docker client (details [here](https://docs.docker.com/engine/install/ubuntu/))
6. Required docker images (*same as for Mac*)
7. tmux
```shell
apt install tmux
```
8. *Optional*: `jq` (only for some functionalities)
```shell
apt install jq
```
9. *Optional*: Monitoring (*same as for Mac*)

# Deployment
1. **Pick a topology (inventory)**
 
Find a topology under `ansible/inventory` (a brief description of the existing topologies [here](./ansible/README.md#Topologies)).

**Important:** Some inventory groups have special requirements.
If these are not installed, the services will fail on runtime:

* `shardsservices`: rocksdb
* `coordinators`: rocksdb
* `peerservices`: Docker client
* `all`: tmux

Depending on the topology you will pick, you can make a local or a remote deployment.

There are two ways to set the inventory as default:
* In the Ansible config file (`ansible/ansible.cfg`) set the `inventory` to the path of the inventory of your preference.
* Create a new Ansible config file (e.g. `ansible/my-ansible.cfg`) that points to your preferred inventory and set the environment variable:
```shell
export ANSIBLE_CONFIG=ansible/my-ansible.cfg
```
2. Build executables and non-executables:
```shell
# During build we access private repo's under github.ibm.com
# A github token with access to these repo's needs to be issued and the following env vars must be set
export SC_GITHUB_USER=...
export SC_GITHUB_TOKEN=...

just setup true true
```
The parameters passed in the aforementioned command define whether we will build the binaries for our local machine, and for the remote machines (using Docker).
Setting both options to `true` will make the build last longer.
* If you only need to deploy on your local machine, you don't need to build the binaries using Docker: `just setup true false` or `just setup true`.
* If you only need to deploy on the remote machines, you don't need to build the binaries for your local: `just setup false true`.
* If you already have the binaries you need (you built them for a previous deployment, and you didn't change your code), you can skip both: `just setup false false` or `just setup`.

The deployment has two stages:
* Build (generate) bins and non-executables (e.g. configs, credentials). These are stored under `eval/deployments`. When a `just` command starts with `build`, then its result will be under this directory.
* Deploy (transfer) these into the corresponding hosts. When a `just` command starts with `deploy` then its result will be on the (remote or local) host. For the case of local deployments, the root directory for all transferred files is `eval/experiments`.

3. Run the services and the clients:
```shell
just run
```
If you don't need to run already the clients, you can only run the servers: `just run services`. Later you can run your clients: `just run clients`.

4. See the `tmux` sessions for all hosts on each machine (or localhost for local deployments):
```shell
tmux ls
```

## Useful commands
* Kill execution
```shell
# Kills all servers
just kill all
# Kills all clients (not services)
just kill clients
# Kills FSC node issuer
just kill issuer
# Kills all orderers
just kill orderingservices
```
* Launch host: `run` command includes some extra commands for the initiation of a channel, chaincode and setting a committer key. If you only want to launch a server:
```shell
# Launches all orderers
just launch orderingservices
# Launches FSC node alice
just launch alice
```

* Clean generated files
```shell
# Cleans all temporary files (e.g. DB files)
just clean all
# Cleans all temporary files for shards
just clean shardsservices
# Cleans all temporary files, and non-executables
just clean all true
# Cleans everything deployed for orderers (temporary files, non-executables, executables)
just clean orderingservices true true 
```
* Call API (only used with the FSC-node REST APIs)
```shell
# Issue 1000 tokens to alice
just call-api issuer issue alice 1000
# Bob asks Alice for 100 tokens with nonce=randomnonce
just call-api bob initiate bob 100 randomnonce
# Alice approves the request from Bob to transfer 100 tokens with nonce=ranodmnonce
just call-api alice transfer bob 100 randomnonce
# Fetch the status of the transaction with nonce=randomnonce
just call-api alice status 0 0 randomnonce
# Fetch Alice's balance
just call-api alice balance
# Fetch all Alices' payments
just call-api alice payments
```
* Add SSH keys to known hosts (generate with `ssh-keygen` if none existing. Choose a name like `id_rsa`, so that it is picked by default)
```shell
ssh-keygen -t ed25519 -C "my_name"
ssh-copy-id -i ~/.ssh/deploy_key.pub root@tokentestbed1.sl.cloud9.ibm.com
...
ssh-copy-id -i ~/.ssh/deploy_key.pub root@tokentestbed15.sl.cloud9.ibm.com
```

# Scalable committer

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
