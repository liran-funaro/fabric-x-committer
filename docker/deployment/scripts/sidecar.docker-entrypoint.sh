#!/usr/bin/env bash

while ! </dev/tcp/mock-ordering-service/4001; do sleep 1; done
while ! </dev/tcp/coordinator/9001; do sleep 1; done
/app start --configs=/config.yaml
