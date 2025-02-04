#########################
# Makefile Targets Summary
#########################

# test: Builds binaries and runs both unit and integration tests
# clean: Removes all binaries
# proto: Generates all committer's API protobufs
# build: Builds all binaries
# build-arch: Builds all binaries for linux/(<cur-arch> amd64 arm64 s390x)
# build-docker: Builds all binaries in a docker container
# docker-builder-run: Executes a command from within a golang docker image.
# docker-runner-image: Builds the scalable-committer docker image containing all binaries
# lint: Runs golangci-lint

#########################
# Constants
#########################

version         := 0.0.2
project_dir     := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
output_dir      ?= $(project_dir)/bin
arch_output_dir ?= $(project_dir)/archbin
cache_dir       ?= $(shell go env GOCACHE)
mod_cache_dir   ?= $(shell go env GOMODCACHE)
go_version      ?= 1.23.4
golang_image    ?= golang:$(go_version)-bookworm
db_image        ?= yugabytedb/yugabyte:2.20.7.0-b58

dockerfile_base_dir       ?= $(project_dir)/docker/images
dockerfile_test_node_dir  ?= $(dockerfile_base_dir)/test_node
dockerfile_release_dir    ?= $(dockerfile_base_dir)/release

# Set this parameter when running docker-builder-run
# E.g., make docker-builder-run cmd="make build-local"
cmd            ?=

# Set the 'docker_cmd' variable directly (e.g., docker_cmd="podman") to override automatic detection.
# If the 'docker_cmd' variable is not set, the script will find and use either Docker or Podman.
# An error will occur if neither container engine is installed.
docker_cmd ?= $(shell command -v docker >/dev/null 2>&1 && echo docker || \
							echo podman || { echo "Error: Neither Docker nor Podman is installed." >&2; exit 1; })
image_namespace=icr.io/cbdc

# Set these parameters to compile to a specific os/arch
# E.g., make build-local os=linux arch=amd64
os             ?= $(shell go env GOOS)
arch           ?= $(shell go env GOARCH)
multiplatform  ?= false
env            ?= env GOOS=$(os) GOARCH=$(arch)
go_build       ?= $(env) go build -buildvcs=false -o

arch_output_dir_rel = $(arch_output_dir:${project_dir}/%=%)

# Set additional parameter to build the test-node for different platforms and push
# E.g., make multiplatform=true docker_push=true build-test-node-image
docker_build_flags=
ifeq "$(multiplatform)" "true"
	docker_build_flags+=--platform linux/amd64,linux/arm64
endif

docker_push ?= false
docker_push_arg = 
ifeq "$(docker_push)" "true"
	docker_push_arg=--push
endif

MAKEFLAGS += --jobs=16

.PHONY: $(MAKECMDGOALS)

#########################
# Quickstart
#########################

test: build
	#excludes integration test. For integration test, use `make integration-test`.
	go list ./... | grep -v "github.ibm.com/decentralized-trust-research/scalable-committer/integration" | xargs go test -v

integration-test: build
	go test -v ./integration/...

test-package-%: build
	go test -v ./$*/...

test-cover:
	go test -v -coverprofile=coverage.profile ./...

test-cover-%:
	go test -v -coverprofile=$*.coverage.profile "./$*/..."

cover-report:
	go tool cover -html=coverage.profile

cover-report-%:
	go tool cover -html=$*.coverage.profile

clean:
	@rm -rf $(output_dir)

kill-test-docker:
	$(docker_cmd) ps -aq -f name=sc_yugabyte_unit_tests | xargs $(DOCKER_CMD) rm -f

#########################
# Generate protos
#########################

PROTO_TARGETS ?= $(shell find ./api \
	 -name '*.proto' -print0 | \
	 xargs -0 -n 1 dirname | xargs -n 1 basename | \
	 sort -u | grep -v testdata \
)

proto: $(PROTO_TARGETS)

proto%:
	@echo "Compiling: $*"
	@protoc --proto_path="${PWD}" \
          --go-grpc_out=. --go-grpc_opt=paths=source_relative \
          --go_out=paths=source_relative:. ${PWD}/api/proto$*/*.proto

#########################
# Binaries
#########################

$(output_dir):
	mkdir -p "$(output_dir)"

$(cache_dir) $(mod_cache_dir):
	# Use the host local gocache and gomodcache folder to avoid rebuilding and re-downloading every time
	mkdir -p "$(cache_dir)" "$(mod_cache_dir)"

BUILD_TARGETS=coordinator signatureverifier validatorpersister sidecar queryexecutor \
							loadgen coordinator_setup mockvcservice mocksigservice mockorderingservice

build: $(output_dir) $(BUILD_TARGETS)

build-arch: build-arch-linux-$(arch) build-arch-linux-amd64 build-arch-linux-arm64 build-arch-linux-s390x

build-arch-%:
	@CGO_ENABLED=0 make \
		os=$(word 1, $(subst -, ,$*)) \
		arch=$(word 2, $(subst -, ,$*)) \
		output_dir=$(arch_output_dir)/$* \
		build

coordinator: $(output_dir)
	$(go_build) "$(output_dir)/coordinator" ./cmd/coordinatorservice

signatureverifier: $(output_dir)
	$(go_build) "$(output_dir)/signatureverifier" ./cmd/sigverification

validatorpersister: $(output_dir)
	$(go_build) "$(output_dir)/validatorpersister" ./cmd/vcservice

sidecar: $(output_dir)
	$(go_build) "$(output_dir)/sidecar" ./cmd/sidecar

queryexecutor: $(output_dir)
	$(go_build) "$(output_dir)/queryexecutor" ./cmd/queryservice

loadgen: $(output_dir)
	$(go_build) "$(output_dir)/loadgen" ./cmd/loadgen

coordinator_setup: $(output_dir)
	$(go_build) "$(output_dir)/coordinator_setup" ./cmd/coordinatorservice/setup_helper

mockvcservice: $(output_dir)
	$(go_build) "$(output_dir)/mockvcservice" ./cmd/mockvcservice

mocksigservice: $(output_dir)
	$(go_build) "$(output_dir)/mocksigservice" ./cmd/mocksigservice

mockorderingservice: $(output_dir)
	$(go_build) "$(output_dir)/mockorderingservice" ./cmd/mockorderingservice

build-docker: $(cache_dir) $(mod_cache_dir)
	$(docker_cmd) run --rm -it \
	  --mount "type=bind,source=$(project_dir),target=$(project_dir)" \
	  --mount "type=bind,source=$(cache_dir),target=$(cache_dir)" \
	  --mount "type=bind,source=$(mod_cache_dir),target=$(mod_cache_dir)" \
	  --workdir $(project_dir) \
	  --env GOCACHE="$(cache_dir)" \
	  --env GOMODCACHE="$(mod_cache_dir)" \
	  $(golang_image) \
    make build output_dir=$(output_dir) env="$(env)"
	scripts/amend-permissions.sh "$(cache_dir)" "$(mod_cache_dir)"


build-test-node-image: build-arch
	${docker_cmd} build \
		$(docker_build_flags) \
		-f $(dockerfile_test_node_dir)/Dockerfile \
	  -t ${image_namespace}/committer-test-node:${version} \
		--build-arg DB_IMAGE=${db_image} \
		--build-arg ARCHBIN_PATH=${arch_output_dir_rel} \
	  . \
		$(docker_push_arg)

build-release-images: build-arch
	./scripts/build-release-images.sh \
		$(docker_cmd) $(version) $(image_namespace) $(dockerfile_release_dir) $(multiplatform) $(arch_output_dir_rel)

build-mock-orderer-image: build-arch
	${docker_cmd} build -f ${dockerfile_release_dir}/Dockerfile \
		-t ${image_namespace}/mock-ordering-service:${version} \
		--build-arg SERVICE_NAME=mockorderingservice \
		--build-arg PORTS=4001 \
		--build-arg ARCHBIN_PATH=${arch_output_dir_rel} \
		.

lint:
	@echo "Running Go Linters..."
	golangci-lint run --color=always --sort-results --new-from-rev=main --timeout=4m
	@echo "Linting Complete. Parsing Errors..."
