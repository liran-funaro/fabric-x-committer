# Ansible Setup Guide

## Inventory Setup

The file `inventory/hosts.yaml` allows control over what remote machines Ansible has access to and how Ansible interacts with them. Variables shared between all machines can be placed under the `vars` field, allowing for example a shared password to be passed to all machines. To share a password for all machines, set the `ansible_ssh_pass` field to the password.

## Shards and Sigverification Services

To designate a machine as a shards service, include it under the `shardsservices` field. To designate a machine as a sigverification service, include it under the `sigverificationservices` field. To include a remote machine, create a field to represent the name of the machine under the designated `hosts` field. Under the field representing the name of the machine, include a field `ansible_host` and set it to the IP address of the machine. If the machine requires a unique password, include a field `ansible_ssh_pass` under the machine name and set it to the password.

## Config Scope Levels

### General Scope

All machines under the `services` field will receive a copy of `config.yaml`. If `config.yaml` does not exist it will not be copied.

### Service Scope

All machines under the `shardsservices` field will receive a copy of `config-shardsservice.yaml`. If `config-shardsservice.yaml` does not exist it will not be copied.<br /><br />All machines under the `sigverificationservices` field will receive a copy of `config-sigverificationservice.yaml`. If `config-sigverificationservice.yaml` does not exist it will not be copied.

### Custom Scope

All machines under either the `shardsservices-custom` or the `sigverificationservices-custom` fields will receive a copy of a custom named config file following the convention `config-[NAME_OF_HOST].yaml`. Custom named config files must exist for each host in the custom fields. If a file does not exist for a host under a custom field Ansible will error.

## Examples

### Simple Example

Here is a simple example of a deployment scheme where there is one shards service machine named machine1 and one sigverification service machine named machine2. Both machines share the same ssh password. Both machines will receive `config.yaml`, machine1 will receive `config-shardsservice.yaml`, and machine2 will receive `config-sigverification.yaml`.

```
all:
  vars:
    ansible_connection: ssh
    ansible_user: "root"
    ansible_ssh_pass: "password123"
  children:
    services:
      children:
        shardsservices:
          hosts:
            machine1:
              ansible_host: "1.23.456.78"
        sigverificationservices:
          hosts:
            machine2:
              ansible_host: "8.76.543.21"
```

### Detailed Example

Here is a more detailed example of a deployment scheme. Notice machine1 has a field for `ansible_ssh_pass`. This machine requires a different ssh password. Notice machine2 and machine4 share the same IP address. This machine will be both a shards service and a sigverification service. In this example, assume the file `config-sigverification.yaml` was removed from the `config` directory. Each machine will receive these configs:<br /><br />
`machine1`: config.yaml, config-shardsservice.yaml
`machine2/machine4`: config.yaml, config-shardsservice.yaml, config-machine4.yaml
`machine3`: config.yaml, config-shardsservice.yaml, config-machine3.yaml
`machine5`: config.yaml, config-machine5.yaml

```
all:
  vars:
    ansible_connection: ssh
    ansible_user: "root"
    ansible_ssh_pass: "password123"
  children:
    services:
      children:
        shardsservices:
          hosts:
            machine1:
              ansible_host: "1.23.456.78"
              ansible_ssh_pass: "password456"
            machine2:
              ansible_host: "8.76.543.21"
          children:
            shardsservices-custom:
              hosts:
                machine3:
                  ansible_host: "3.33.333.33"
        sigverificationservices:
          children:
            sigverificationservices-custom:
              hosts:
                machine4:
                  ansible_host: "8.76.543.21"
                machine5:
                  ansible_host: "1.22.333.44"
```

In this example we have a machine that is both a shards and sigverification service. Config files for shards services and sigverification services are placed in separate directories so that they do not conflict. Similar to how service scope config files work, if the `config.yaml` file were to be removed from the `config` dir then no machine would have a copy of it.