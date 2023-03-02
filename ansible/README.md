# Ansible Guide

Make sure you have installed Ansible and all dependencies as defined [here](../README.md#Prerequisites).

## General about components

### Inventory

Each inventory file (`ansible/inventory/hosts-*.yaml`) defines a topology, i.e. a set of hosts that:
* are assigned to a group with a well-known name. The group determines their functionality (e.g. `coordinators`).
* define some well-known properties depending on their group. These properties determine their behavior (e.g. `service_port`)

```yaml
all:
  children:
    hosts:
      webserver-machine1:
        ansible_host: tokentestbed1.ibm.com
        rest_port: 8080
        admin_port: 5000
      webserver-machine2:
        ansible_host: tokentestbed2.ibm.com
        rest_port: 8080
        admin_port: 5000
      webclient-machine1:
        ansible_host: tokentestbed3.ibm.com
        ui_port: 8000
        admin_port: 5000
```

We can create groups of hosts, in order to refer to them with a single group name, instead of concatenating the name of each machine.
For example, `webserver-machine1` and `webserver-machine1` could be under a group `webservers`.
This way, we can deploy or launch all web servers by referring to them as `webservers`.
This will allow seamless addition/removal of hosts in the future.
The highest-level group in the `hosts` hierarchy is `all`.

To avoid repetition, properties can be extracted to the highest group whose members all share the same property value.

Applying both simplifications would result in the following inventory:

```yaml
all:
  vars:
    # Apply to all
    admin_port: 5000
  children:
    webservers:
      vars:
        # Apply to all webservers
        rest_port: 8080
      hosts:
        webserver-machine1:
          # Apply only to webserver-machine1
          ansible_host: tokentestbed1.ibm.com
        webserver-machine2:
          ansible_host: tokentestbed2.ibm.com
    webclient-machine1:
      ansible_host: tokentestbed3.ibm.com
      ui_port: 8000
```

### Playbooks
Each playbook is a set of commands to be executed on one or more hosts.
The input to a playbook is an inventory and optionally a set of parameters for further fine-tuning of the execution.
A playbook has access to the group, as well as the parameters of each host and can define the build process, creation config files, copy resources to a host, run an executable, etc.
Thus, depending on the combination of a playbook and an inventory, we can achieve deployments of different topologies.

### Ansible config
Whenever we execute a playbook, we have to pass the inventory as an input parameter.
Otherwise, the one defined in the Ansible config will be taken by default.
The config is defined by setting `$ANSIBLE_CONFIG` to point to its path.
Currently, we use `ANSIBLE_CONFIG=./ansible/ansible.cfg` by default.

## About SC components

### Inventory

In our inventories we will find the following supported groups and properties:

#### Services:

* 0 or 1 `coordinators`
```yaml
  # Optional
  # The coordinator contains the interface to set the verification key of the sig verifiers.
  # This key is set using the setup_helper of the coordinator.
  # It can be either a newly-generated key, or the key of an FSC endorser node (for the cases of complete deployments).
  # If this property is set, then the endorser credentials will be copied to the coordinator host during deployment,
  # and the key will be set during runtime.
  # Otherwise another external node (e.g. blockgens, sidecarclients) has to call SetVerificationKey().
  inherit_endorser: endorser-1
  # How we connect to the machine.
  # Supported values: local, ssh
  ansible_connection: ssh
  # The address of the host
  ansible_host: tokentestbed12.ibm.com
  # The user to use when connecting to the host
  ansible_user: root
  # The path to the directory where binaries will be copied during deployment.
  binary_dir: /root/bin/
  # The base config under the /configs directory to take when deploying.
  # Some parameters defined in this config depend on the topology (e.g. the listen port, the sig-verifier endpoints).
  # These parameters (indicated in inline comments) will be overwritten.
  config: config-coordinator.yaml
  # The path to the directory where configs, credentials, blockchains will be copied during deployment.
  config_dir: /root/config
  # The OS of the machine that the service is running on.
  # Depending on the os, the right binary file will be deployed.
  # Supported values: osx, linux #TODO: AF Change to local instead of osx
  os: linux
  # The port where prometheus exporter exposes its metrics.
  prometheus_exporter_port: 2112
  # The port we listen to for incoming connections.
  service_port: 5002
```
* 0, 1 or more `sigservices`
```yaml
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config: config-sigservice.yaml
  config_dir: /root/config
  os: linux
  prometheus_exporter_port: 2112
  service_port: 5000
```
* 0, 1 or more `shardsservices`
```yaml
  # The path to store the DB files of the shards.
  db_dir: /root/config/db
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config: config-shardsservice.yaml
  config_dir: /root/config
  os: linux
  prometheus_exporter_port: 2112
  service_port: 5001
```
  * 0, 1 or more `orderingservices`
```yaml
  # Orderer organization
  organization: DefaultOrdererOrg
  # Domain of organization
  domain: example.com
  # Listen port
  service_port: 7050
  # Cluster port
  cluster_port: 7070
  # Operations Port (including prometheus)
  ops_port: 2112
  # The config file under the appropriate path in the NWO artifacts
  config: orderer.yaml
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config_dir: /root/config
  os: linux
```
* 0, 1 or more `peerservices`
```yaml
  # The profile file under the appropriate path in the NWO artifacts.
  # It will be used by the hosts that define the inherit_peer property for their connection to the orderers.
  profile: profile.yaml
  organization: DefaultPeerOrg
  domain: example.com
  service_port: 7050
  config: core.yaml
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config_dir: /root/config
  os: linux
```
* 0, 1 or more `fscservices`
```yaml
  # Optional (defaults to false)
  # Used for the bootstrap FSC node that has to run first.
  bootstrap: true
  # Optional (defaults to false)
  # Defines a node as auditor.
  auditor: true
  # Optional (defaults to false)
  # Defines a node as certifier.
  certifier: true
  # Optional (defaults to false)
  # Defines a node as certifier.
  # A coordinator node can refer to a node with endorser=true with "inherit_endorser".
  endorser: true
  # Optional
  # Defines view factories to be registered in the FSC main.
  # Factories with an ID that starts with "init" will be also instantiated and called within the main.
  view_factories:
    - id: initRegisterAuditor
      factory: github.ibm.com/decentralized-trust-research/fts-sc/demo/views/RegisterAuditorViewFactory
    - id: transfer
      factory: github.ibm.com/decentralized-trust-research/fts-sc/demo/views/TransferViewFactory
  # Optional
  # Defines responder-initiator pairs to be registered in the FSC main.
  responders:
    - responder: github.ibm.com/decentralized-trust-research/fts-sc/demo/views/AcceptIssuedCashView
      initiator: github.ibm.com/decentralized-trust-research/fts-sc/demo/views/IssueCashView
  # Optional
  # The issuer identities (if the node is an issuer).
  # "default" is a reserved ID for a default issuer identity.
  issuer_identities:
    - default
    - id1
  # Optional
  # The owner identities.
  # "default" is a reserved ID for a default owner identity.
  owner_identities:
    - default
    - id1
  # Optional
  # Port used for the REST API interface.
  # If the port is defined, a REST API server will also be launched as part of the main.
  ops_port: 8081
  # P2P port
  p2p_port: 8021
  # Web port
  web_port: 9081
  organization: DefaultPeerOrg
  domain: example.com
  service_port: 7050
  config: core.yaml
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config_dir: /root/config
  os: linux
```
* 0 or 1 `sidecars`
```yaml
  # The host needs credentials to connect to the orderers.
  # These credentials are inherited by the peer indicated here by its inventory_hostname.
  inherit_peer: peerservice-machine1
  organization: DefaultPeerOrg
  domain: example.com
  service_port: 7050
  config: config-sidecar.yaml
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config_dir: /root/config
  os: linux
  prometheus_exporter_port: 2112
```
#### Clients
* 0, 1 or more `sidecarclients`
```yaml
  # Optional
  # The profile for the traffic generation.
  # If none defined, the client does not send any load.
  profile: profile-sidecar-client.yaml
  inherit_peer: peerservice-machine1
  config: config-sidecar-client.yaml
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config_dir: /root/config
  os: linux
  prometheus_exporter_port: 2112
```
* 0, 1 or more `blockgens`
```yaml
  profile: profile-blockgen.yaml
  config: config-blockgen.yaml
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config_dir: /root/config
  os: linux
  prometheus_exporter_port: 2112
```
* 0, 1 or more `ordererlisteners`
```yaml
  inherit_peer: peerservice-machine1
  config: config-orderer-listener.yaml
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config_dir: /root/config
  os: linux
  prometheus_exporter_port: 2112
```
* 0, 1 or more `orderersubmitters`
```yaml
  inherit_peer: peerservice-machine1
  config: config-orderer-submitter.yaml
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config_dir: /root/config
  os: linux
  prometheus_exporter_port: 2112
```

#### Other

* 0 or 1 `monitoring`
```yaml
  # The port to which the Jaeger traces should be sent.
  jaeger_exporter_port: 14268
  ansible_connection: ssh
  ansible_host: tokentestbed12.ibm.com
  ansible_user: root
  binary_dir: /root/bin/
  config_dir: /root/config
  os: linux
```

### Topologies
The current inventories cover different type of topologies:
* `hosts-admin-ops.yaml`: Only for the admin (housekeeping) operations
* `hosts-committer-experiment(-local).yaml`: Experiment to measure the performance of the SC with a full SC and a blockgen as optional client.
* `hosts-ordering-experiment(-local).yaml`: Experiment to measure the performance of the ordering service with orderers, and orderer-listeners/orderer-submitters as clients.
* `hosts-complete(-local).yaml`: Full deployment with SC and sidecar, orderers, Fabric peers, FSC nodes, and sidecar-client as optional client.

### Playbooks
We have the following types of playbooks:
* **[0, 20) - Admin:** Execute housekeeping operations:
  * update dependencies
  * update passwords
* **[20, 40) - Build non-executables:** Create config files and NWO artifacts (credentials, configs, chaincodes, genesis blocks).
This includes:
  * copying the base configs as indicated by `config`
  * modifying the topology-related parameters, e.g. endpoints, profile paths
  * generating an NWO topology from the inventory
  * using this NWO topology to generate configs, credentials, genesis blocks, chaincodes, FSC node mains
* **[40, 60) - Deploy non-executables:** Send configs and NWO artifacts to the corresponding hosts.
Sometimes this includes modifications in the paths included in the config files, so that they point to their new destinations.
* **[60, 80) - Launch services:** Run binaries on the hosts
* **[80, 100) - Utils:**
  * kill all services running
  * clean files created on runtime (e.g. DB files), non-executables, and binaries