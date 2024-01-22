#!/usr/bin/env bash

while !</dev/tcp/vcservice/6001; do sleep 1; done; \
  /app start --configs=/config.yaml

