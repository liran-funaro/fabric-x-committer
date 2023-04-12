# Prerequisites

## Backend
### Deploy and Run Topology
First you will need to create a Github token on github.ibm.com as described [here](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/creating-a-personal-access-token). Make sure to tick **repo**, as described in step 8.
This will allow access to the private repos.
This token is the one to use instead of `<<YOUR_TOKEN_HERE>>` when setting the env var SC_GITHUB_TOKEN.
* Build binaries, configs, and run topology: You have changed the code or the topology. You can pass `BFT` or `etcdraft` as a parameter to the `setup` method.
```shell
export SC_FSC_PASSWORD_FILE=/path/to/file # If not passed, passwords.yml will be used by default
export SC_GITHUB_USER=alexandros-filios
export SC_GITHUB_TOKEN=<<YOUR_TOKEN_HERE>>
just setup BFT false true
just run
```
* Build configs, and run topology. You have changed config files, but not the code or the topology.
```shell
export SC_FSC_PASSWORD_FILE=/path/to/file # If not passed, passwords.yml will be used by default
export SC_GITHUB_USER=alexandros-filios
export SC_GITHUB_TOKEN=<<YOUR_TOKEN_HERE>>
just setup BFT
just run
```
* Run topology: You haven't made any changes to any config, the code, or the topology.
```shell
just kill
just clean
just run
```

Check that all servers are up and running:
* On Grafana
* On the remote machine (`tokentestbed16.sl.cloud9.ibm.com`) in the `scalable-committer` project:
```shell
just check-ports
```

### Ping all servers
```shell
just ping
```

### Limit load rate
```shell
just limit-rate 30000
```

### Invoke REST-API
```shell
# Issue 100 tokens to alice
just call-api issuer issuer issue alice 100
# Withdraw 100 tokens for alice
just call-api banka alice withdraw '' 100
# Request 10 tokens for bob
just call-api bankb bob initiate bob 10 donut
# Accept to transfer 10 tokens to bob from alice's account
just call-api banka alice transfer bob 10 donut
# Check transaction status by its nonce
just call-api banka alice status '' '' donut
# Check alice's balance
just call-api banka alice balance
# Check alice's payments
just call-api banka alice payments
# Check wallets managed by banka
just call-api banka banka wallets
# Check validation records
just call-api endorser-1 seadmin records
```


### Build and Run Web UI
The config with the FSC nodes and their endpoints is available on http://tokentestbed16.sl.cloud9.ibm.com:8080/ui-config.json.
In the `fts-sc` project on your local machine:
```shell
cd demo/app/ui
docker build -t ui-token .
docker tag ui-token:latest docker-eu.artifactory.swg-devops.com/res-decentralized-trust-team-sc-docker-local/ui-token:latest
docker push docker-eu.artifactory.swg-devops.com/res-decentralized-trust-team-sc-docker-local/ui-token:latest
```
On the remote machine in any directory (`tokentestbed16.sl.cloud9.ibm.com`):
```shell
docker run -p 8081:80 docker-eu.artifactory.swg-devops.com/res-decentralized-trust-team-sc-docker-local/ui-token
```
The UI will be available on http://tokentestbed16.sl.cloud9.ibm.com:8081/.

## Troubleshooting
* Check all servers are up and running
```shell
just check-ports
```
* Check that there were no errors reported during `just run`. Usually errors during run are because of errors during `just setup`. The most common errors:
  * During `just run`, the hostname of a server could not be temporarily resolved. It is a rather rare problem, so just retry the deployment.
  * During `just build`, the binary files cannot be found under `FAB_BINS`. Make sure you have set the env vars and downloaded the package as described at the beginning.
* Make sure that on the remote server (`http://tokentestbed16.sl.cloud9.ibm.com`) you have all Linux binaries, configs, and orderer-artifacts under `eval/deployments`.
* When an experiment crashes because the hard disk of a machine (an orderer) is full, the server might become unresponsive until you remove manually some files.

## Notes
* When you change the topology
  * Rebuild binaries and configs before running
  * Rebuild UI-config (`just serve-ui-config`)
  * You don't need to change the Web UI. Just refresh the page.
  * Make sure that the machines you assigned to the shards and coordinator have rocksdb installed. Currently, rocksdb is installed on tokentestbed9-14.
* If you rebuild the Web UI, no change is needed neither on the backend nor on the UI-config.
* When you open a new session or a tmux window, make sure you re-set the environment variables before building.
* For servers with public IP's, we need to limit the accessibility of the exposed ports to the minimum. These ports may change depending on our topology. Taking one of our topologies as an example, all components will communicate with each other using their private IP's, while all ports of their public IP's will be blocked. It is essential that *only* following ports be kept open:
  * `banka-host:8083`: REST API Port for BankA, accessible through the mobile app
  * `bankb-host:8084`: REST API Port for BankB, accessible through the mobile app
  * `deploy-host:22`: SSH Port for deployment server
  * `deploy-host:3001`: HTTPS Port for Grafana UI
  * `deploy-host:8081`: HTTP Port for WebUI

