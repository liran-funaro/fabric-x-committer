#!/bin/bash

set -ex
curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v23.4/protoc-23.4-linux-x86_64.zip
unzip -j protoc-23.4-linux-x86_64.zip 'bin/*' -d "$HOME/bin"

go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
