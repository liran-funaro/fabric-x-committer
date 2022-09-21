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
SERVICES_CONFIG_PATH="${rootDir}/config"
GENERAL_CONFIG="config.yaml"
SHARDS_CONFIG="config-shardsservice.yaml"
SIGVER_CONFIG="config-sigverification.yaml"

export ANSIBLE_CONFIG=${ANSIBLE_CONFIG_PATH}
ansible-playbook "${PLAYBOOKS_PATH}/0-setup-dir.yaml"
if [ -f "${SERVICES_CONFIG_PATH}/${GENERAL_CONFIG}" ]; then
    ansible-playbook "${PLAYBOOKS_PATH}/10-copy-general-config.yaml" --extra-vars "filename=${GENERAL_CONFIG}"
fi
if [ -f "${SERVICES_CONFIG_PATH}/${SHARDS_CONFIG}" ]; then
    ansible-playbook "${PLAYBOOKS_PATH}/20-copy-service-config.yaml" --extra-vars "servicename=shards filename=${SHARDS_CONFIG}"
fi
if [ -f "${SERVICES_CONFIG_PATH}/${SIGVER_CONFIG}" ]; then
    ansible-playbook "${PLAYBOOKS_PATH}/20-copy-service-config.yaml" --extra-vars "servicename=sigverification filename=${SIGVER_CONFIG}"
fi
ansible-playbook "${PLAYBOOKS_PATH}/30-copy-custom-config.yaml"
ansible-playbook "${PLAYBOOKS_PATH}/40-copy-and-run-service.yaml" --extra-vars "arg=$1"

./${COORDINATOR_PATH}