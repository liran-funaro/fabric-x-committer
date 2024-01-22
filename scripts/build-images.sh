#!/usr/bin/env bash
set -euxo pipefail

project_dir=.
bin_dir=./bin
image_prefix=sc_
base_image="registry.access.redhat.com/ubi8/ubi-micro:latest"

function build_image () {
  local service_name=$1
  local service_ports=$2

# TODO set target platform
  docker build -f ${project_dir}/docker/Dockerfile \
    -t "${image_prefix}${service_name}" \
    --build-arg BASE_IMAGE="${base_image}" \
    --build-arg APP_BIN="${bin_dir}/${service_name}" \
    --build-arg PORTS="${service_ports}" \
    .
}


# to build the sc binaries for z, make sure you use the 
# following make command before building the container images
#
# make GOOS=linux GOARCH=s390x build

# build container images
build_image coordinator "9001 2110"
build_image sigservice "5001 2110"
build_image vcservice "6001 2110"
build_image blockgen "2110"

docker images | grep sc_

