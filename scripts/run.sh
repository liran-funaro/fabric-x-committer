#!/bin/bash

rootDir="scalable-committer"

workingDir="$(basename "$(pwd)")"
while [ "$workingDir" != "$rootDir" ]
do
    cd ..
    workingDir="$(basename "$(pwd)")"
done

rootDir="$(pwd)"

ANSIBLE_CONFIG_PATH="${rootDir}/ansible/ansible.cfg"
PLAYBOOKS_PATH="${rootDir}/ansible/playbooks"
COORDINATOR_PATH="bin/coordinator"

export ANSIBLE_CONFIG=${ANSIBLE_CONFIG_PATH}
ansible-playbook "${PLAYBOOKS_PATH}/50-run-service-bin.yaml" --extra-vars "arg=$1"

./${COORDINATOR_PATH}