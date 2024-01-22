#!/usr/bin/env bash

while !</dev/tcp/coordinator/9001; do sleep 1; done; \
  /app start --configs=/config.yaml

