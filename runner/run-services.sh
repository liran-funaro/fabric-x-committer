#!/bin/bash

/root/bin/sigservice --configs /root/config/sigservice-machine-config-sigservice.yaml &
/root/bin/shardsservice --configs /root/config/shardsservice-machine-config-shardsservice.yaml &
sleep 1 # Wait until the services are up and running before starting the coordinator
/root/bin/coordinator --configs /root/config/coordinator-machine-config-coordinator.yaml