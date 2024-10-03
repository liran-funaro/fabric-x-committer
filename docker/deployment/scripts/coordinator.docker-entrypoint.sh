#!/usr/bin/env bash

while ! </dev/tcp/signature-verifier/5001; do sleep 1; done

while ! </dev/tcp/validator-persister/6001; do sleep 1; done
/app start --configs=/config.yaml
