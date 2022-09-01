#!/bin/bash

CONFIG_PATH="ansible/ansible.cfg"
PLAYBOOK_PATH="ansible/playbooks/10-copy-and-run.yaml"
COORDINATOR_PATH="bin/coordinator"

export ANSIBLE_CONFIG=${CONFIG_PATH}
ansible-playbook ${PLAYBOOK_PATH}

./${COORDINATOR_PATH}