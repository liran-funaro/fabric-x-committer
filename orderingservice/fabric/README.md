# Fabric Orderer evaluation

We assume you have set `export SC=$GOPATH/src/github.ibm.com/decentralized-trust-research/scalable-committer`.

## Patch Fabric and build

```bash
export FABRIC_PATH=$GOPATH/src/github.com/hyperledger/fabric
git clone https://github.com/hyperledger/fabric.git $FABRIC_PATH
cd $FABRIC_PATH
git checkout v2.4.7 -b v2.4.7-branch
git apply $SC/orderingservice/fabric/orderer_no_sig_check.patch
make native
```

## Prepare and run

```bash
just init
just start
```

`just init` creates a fresh set of credentials for the ordering service and another org for the "clients",
and produces a genesis block to start the blockchain.
Note that `just init` is a shortcut for `just clean cryptogen genesis`.

`just start` uses `tmux` to create a few windows, where three ordering nodes are started by default.

You can start the client listener with `just listen`.
To submit transactions to the ordering service, run `just submit`.

You can kill all processes (i.e., ordering nodes, listener, and submitter) via `just kill`.

## Configuration

- A common configuration file for the ordering nodes is given in `orderer.yaml`.
Node specific settings are set via environment variables (see justfile target: `run_orderer`).

- The consortium is defined via the `crypto-config.yaml`, which is used as input for `just cryptogen`.
The generated output is located in `out/`.

- The genesis block is defined in `configtx.yaml`, which is used as input for `just genesis`.
The generated output is located in `out/`.


## TODOS

- [ ] Prepare for remote deployment (ansible, tls certs, etc ...)
- [ ] Integrate scalable committer workload gen into `client/cmd/submitter`.
- [ ] Integrate mock-coordinator and coordinator into `clients/cmd/listener`.
- [ ] Add metrics to measure throughput and latency.
- [ ] Establish a baseline for the ordering service
- [ ] Try to reproduce 10k TPS as reported by Yacov (this includes envelope sig verification)
- [ ] Try to have move TPS by removing orderer sig verification