# Prerequisites

| Dependency                  | Mac                                                                                                                                                                                                                                                                                                                                                                                             | Ubuntu                                                                                                                                                                                                                                                                                                                                                                                                                              | Details                                                                                                           |
|-----------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------|
| Just                        | `brew install just`                                                                                                                                                                                                                                                                                                                                                                             | Install just:<br/>`curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh &#124; bash -s -- --to /usr/local/just`<br/><br/>Add the following line to `~/.profile`:<br/>`export PATH=$PATH:/usr/local/just`<br/><br/>Test the version:<br/>`just —-version`                                                                                                                                                            | [Further details](https://github.com/casey/just#installation)                                                     |
| Golang 1.20                 | `brew install go@1.20`                                                                                                                                                                                                                                                                                                                                                                          | `apt-get update`<br/>`wget https://go.dev/dl/go1.20.3.linux-amd64.tar.gz `<br/>`tar xvf ./go1.20.3.linux-amd64.tar.gz`<br/>`rm ./go1.20.3.linux-amd64.tar.gz`<br/>`mv ./go/ /usr/local/`<br/><br/>Add the following lines in `~/.profile`:<br/>`export GOPATH=$HOME/go`<br/>`export PATH=$PATH:/usr/local/go/bin:$GOPATH/bin`<br/><br/>Test the version:<br/>`go version`                                                           |                                                                                                                   |
| rocksdb 7.9.2 and gorocksdb | Install rocksdb: <br /> `wget https://raw.githubusercontent.com/Homebrew/homebrew-core/d23af025f1956dff0a6afade287192b61915cf09/Formula/rocksdb.rb` <br /> `brew install --build-from-source ./rocksdb.rb` <br /><br /> Install gorocksdb: <br/> `go get -u github.com/linxGnu/grocksdb@v1.7.15` <br /><br /> Make dynamic linker to pick the shared library: <br /> `update_dyld_shared_cache` | `apt-get update && apt-get install -y \ `<br/>`      libgflags-dev \ `<br/>`      libsnappy-dev \ `<br/>`      zlib1g-dev \ `<br/>`      libbz2-dev \ `<br/>`      libzstd-dev \ `<br/>`      liblz4-dev`<br/>`cd /tmp && git clone https://github.com/facebook/rocksdb.git `<br/>`cd /tmp/rocksdb && git checkout 7555243bcfb7086e8bad38d43a518ff4c53dc17a && DEBUG_LEVEL=0 make shared_lib && make install-shared`<br/>`ldconfig` | Not required if you never build for the local machine, because the Docker image contains the dependency           |
| Docker client               | `brew install docker`                                                                                                                                                                                                                                                                                                                                                                           | Follow instructions [here](https://docs.docker.com/engine/install/ubuntu/)<br/><br/>To grant access to a non-root user:<br/>`apt install acl`<br/>`setfacl --modify user:cbdcdemo:rw /var/run/docker.sock`                                                                                                                                                                                                                          |                                                                                                                   |

# Quickstart

* Build and run local binaries
```shell
just build-local
./out/local/sigservice --configs config/config-sigservice.yaml
./out/local/shardsservice --configs config/config-shardsservice.yaml
./out/local/coordinator --configs config/config-coordinator.yaml
./out/local/blockgen stream --configs config/config-blockgen.yaml
```

* Build linux binaries for remote machines
```shell
just build-linux
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
