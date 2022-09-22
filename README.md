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
sig_verification:
  servers: "localhost"
shards_service:
  servers: "localhost"
  num_shards_per_server: 1
  delete_existing_shards: true
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
