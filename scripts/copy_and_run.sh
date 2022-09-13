#!/bin/bash

rootDir="scalable-committer"

workingDir="$(basename "$(pwd)")"
while [ "$workingDir" != "$rootDir" ]
do
    cd ..
    workingDir="$(basename "$(pwd)")"
done

rootDir="$(pwd)"

CONFIG_PATH="${rootDir}/ansible/ansible.cfg"
PLAYBOOK_PATH="${rootDir}/ansible/playbooks/10-copy-and-run.yaml"
COORDINATOR_PATH="bin/coordinator"

export ANSIBLE_CONFIG=${CONFIG_PATH}
ansible-playbook ${PLAYBOOK_PATH} --extra-vars "arg=$1"

./${COORDINATOR_PATH}