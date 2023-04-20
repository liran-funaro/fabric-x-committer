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
just replace-orderer-bins /home/cbdcdemo/orderer #Workaround until we start building the orderers from the source code
just replace-issuer-bins /home/cbdcdemo/issuer #Workaround until we start building the issuers from the source code
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
export SC_GITHUB_USER=alexandros-filios
export SC_GITHUB_TOKEN=<<YOUR_TOKEN_HERE>>
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

## Firewall Rules
If the hosts in our setup have both public and private IP's, we just need to access a few ports from the Internet. The rest of the communication can take place using the private IP's.

### Update `iptables`

* Connect to the machine using its private IP (we will later block the SSH port for the public IP and that would log us out)
```shell
ssh cbdcdemo@ttbed4.frankfurt2
```
* Check the interface that corresponds to the public IP (in our case it is `bond1`):
```shell
ifconfig
```
* Add the new chain and its corresponding rules. In this case, we want only `tcp:8081` to be publicly available:
```shell
iptables -N chain-cbdc # Create a new chain (if not existing)
iptables -F chain-cbdc # Remove all rules from the chain (if existing)
iptables -A INPUT -j chain-cbdc # Add a reference to the INPUT chain, so that this jumps to our custom chain
iptables -A chain-cbdc -i bond1 -p tcp --dport 8081 -j ACCEPT # Accept requests to tcp:8081
iptables -A chain-cbdc -i bond1 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT # Accept requests if a connection has been already opened from the server
iptables -A chain-cbdc -i bond1 -j DROP # Drop all other incoming requests
iptables -S chain-cbdc # Show all rules for the chain
```

### Topology requirements
*Disclaimer:* The following commands are correct and can be copied and pasted, but pay attention to the interface.
Make sure that the interface (`bond1` in the following examples) is indeed the one that corresponds to the public IP, using `ifconfig`.

For our demo topology, it is essential that *only* following ports be kept open for the public IP:
* `lib-p2p`, `dw`, `issuer`, `banka`, `bankb`, `endorser`
  * `p2p_port`: Port for the P2P communication of the FSC nodes
  * `ops_port`: Port of the REST API (not applicable for `lib-p2p`)
* `deploy-host`
  * `tcp:22`: SSH Port for deployment server
  * `tcp:3001`: HTTPS Port for Grafana UI
  * `tcp:8081`: HTTP Port for WebUI
  * All established connections (for Docker operations)

For example, on the deploy host:

```shell
sudo su
iptables -N chain-cbdc # Only necessary the first time
iptables -F chain-cbdc
iptables -A INPUT -j chain-cbdc
iptables -A chain-cbdc -i bond1 -p tcp --dport 22 -j ACCEPT
iptables -A chain-cbdc -i bond1 -p tcp --dport 3001 -j ACCEPT
iptables -A chain-cbdc -i bond1 -p tcp --dport 8081 -j ACCEPT
iptables -A chain-cbdc -i bond1 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A chain-cbdc -i bond1 -j DROP
iptables -S chain-cbdc
```

For the host where `lib-p2p`, `issuer`, and `endorser` reside:

```shell
sudo su
iptables -N chain-cbdc # Only necessary the first time
iptables -F chain-cbdc
iptables -A INPUT -j chain-cbdc # Only necessary the first time
iptables -A chain-cbdc -i bond1 -p tcp --dport 8020 -j ACCEPT
iptables -A chain-cbdc -i bond1 -p tcp --dport 8022 -j ACCEPT
iptables -A chain-cbdc -i bond1 -p tcp --dport 8082 -j ACCEPT
iptables -A chain-cbdc -i bond1 -p tcp --dport 8025 -j ACCEPT
iptables -A chain-cbdc -i bond1 -p tcp --dport 8085 -j ACCEPT
iptables -A chain-cbdc -i bond1 -j DROP
iptables -S chain-cbdc
```

*Important:* If a host is both `endorser-1` and `issuer`, make sure to accept both required ports.

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
* If token transfers (using `call-api`) are successful but no data is shown on Grafana (the sidecar client failed), make sure you have the correct orderer binary.
* If any problem occurs, make sure you have set the Github user and token env variables.

## Notes
* When you change the topology
  * Rebuild binaries and configs before running
  * Rebuild UI-config (`just serve-ui-config`)
  * You don't need to change the Web UI. Just refresh the page.
  * Make sure that the machines you assigned to the shards and coordinator have rocksdb installed. Currently, rocksdb is installed on tokentestbed9-14.
* If you rebuild the Web UI, no change is needed neither on the backend nor on the UI-config.
* When you open a new session or a tmux window, make sure you re-set the environment variables before building.

