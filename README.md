# scalable-committer

## Setup

### Mac

```bash
brew install rocksdb
```

## Build

```bash
# build all binaries and store them in ./bin
just build-all

# set linux as build target
GOOS=linux just build-all
```

## Setup

```bash
# setup up all remote machines as shards and sigverification services by copying over config and bin files
just setup
```

## Run

```bash
# execute all shards and sigverification services then execute coordinator
just run
```

## Start

```bash
# build, setup, and run all in one line
just start
```


## Run e2e locally

```bash
just build-all

# generates ~1gb trace (see profile1.yaml) for details
./bin/blockgen generate -p wgclient/testdata/profile1.yaml -o wgclient/out/blocks
```

Next, create `config-coordinator.yaml` with the following content:

```yaml
coordinator:
  sig-verifiers:
    endpoints:
      - "localhost:5000"
  shards-servers:
    servers:
      - endpoint: "localhost:5001"
        num-shards: 1
  delete-existing-shards: true
  prefix-size-for-shard-calculation: 2
```

Next we start the services, each in a terminal window:

- Sig service
```bash
./bin/sigservice
```

- Shards service
```bash
./bin/shardservice
```

- coordinator service
```bash
./bin/coordinator
```

- workload
```bash
./bin/blockgen pump --host=localhost --port=5002 --in=./wgclient/out/blocks
```

### Debugging notes

You can set the logging level for each service via `SC_LOGGING_LEVEL=debug`.

Example:
```bash
SC_LOGGING_LEVEL=debug ./bin/shardservice
```

## Docker based builder

As we target Linux servers, all our SC components must be compiled for Linux.
However, since most of us are using a Mac, compiling the shards server using `GOOS=linux go build` is not enough.
For that reason, we provide a docker container that can be used to build the SC component binaries.

```bash
# builds the docker image
just docker-image

# starts a bash in the container
just docker bash

# runs `just build-all` inside the container
just docker just build-all
```

## Full build

After having a Docker-based builder set up, we can build and deploy the code and the base configs (as defined under `config/`) to all of the machines defined in the inventory as follows:

```bash
just deploy-base-setup
```

## Experiments

An experiment consists in setting up and starting all the servers (coordinator, sig verifiers and shard servers), as well as a block generator that acts as the client. We can have several variations of an experiment by modifying:

* The number of sig verifiers (by default 3)
* The number of shard servers (by default 3)
* The percentage of large TXs (with 8 SNs instad of 2 SNs) (by default 0%)
* The percentage of TXs that have valid signatures (by default 100%)

The following command will modify and deploy the necessary modified config files and start up all the servers in the correct order:

```bash
just run-experiment sig_verifiers=("3") shard_servers=("3") large_txs=("0.0") validity_ratio=("1.0"):
```

An experiment suite is a set of experiments we run sequentially in an automated manner for a specific duration.
When we run an experiment suite, the values of the config variables, as well as the start time is logged in an experiment-tracking-log file.
We can run an experiment suite with all combinations of the aforementioned configuration variables as follows:

```bash
just run-experiment-suite  experiment_name shard_servers_arr=("3") large_txs_arr=("0.0") validity_ratio_arr=("1.0") experiment_duration=(experiment-duration-seconds)
```

The experiment-tracking-log will be stored under `eval/experiments/trackers/{{experiment_name}}.txt`.

Once an experiment suite has finished, the experiment-tracking-log will be parsed and for each experiment run, the Prometheus instance that collected the metrics for the experiment will be queried for some metrics of interest, as defined under `utils/experiment/main`.
The results will be stored under `eval/experiments/results/{{experiment_name}}.txt`.

For the evaluation of our system, we have defined some experiment suites that we can run directly with the following shortcut commands:

```bash
just run-variable-shard-experiment
just run-variable-tx-sizes-experiment
just run-variable-validity-ratio-experiment
```