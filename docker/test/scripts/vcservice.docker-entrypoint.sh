#!/usr/bin/env bash

while !</dev/tcp/db/5432; do sleep 1; done; \
  /app init --configs=/config.yaml && \
  /app start --configs=/config.yaml

