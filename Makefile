#########################
# Makefile Targets Summary
#########################

# test: Builds binaries and runs both unit and integration tests
# clean: Removes all binaries
# protos-coordinator: Generates coordinator protobufs
# protos-token: Generates token protobufs
# protos-blocktx: Generates block and transaction protobufs
# protos-wgclient: Generates wgclient protobufs
# build: Builds all binaries
# build-docker: Builds all binaries in a docker container
# docker-builder-run: Executes a command from within a golang docker image.
# docker-runner-image: Builds the scalable-committer docker image containing all binaries
# lint: Runs golangci-lint

#########################
# Constants
#########################

version        := 0.0.2
project_dir    := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
output_dir     ?= $(project_dir)/bin
cache_dir      ?= $(shell go env GOCACHE)
mod_cache_dir  ?= $(shell go env GOMODCACHE)
go_version     ?= 1.23.2
golang_image   ?= golang:$(go_version)-bookworm
db_image       ?= yugabytedb/yugabyte:2.20.7.0-b58

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
# Eg.g, make build-local os=linux arch=amd64
os             ?= $(shell go env GOOS)
arch           ?= $(shell go env GOARCH)
multiplatform  ?= false
env            ?= env GOOS=$(os) GOARCH=$(arch)
go_build       ?= $(env) go build -buildvcs=false -o


.PHONY: test clean

#########################
# Quickstart
#########################

test: build
	#excludes integration test. For integration test, use `make integration-test`.
	go list ./... | grep -v "github.ibm.com/decentralized-trust-research/scalable-committer/integration/test/" | xargs go test -v

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

protos-coordinator:
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		--proto_path=. \
		--proto_path=./token \
		--proto_path=./coordinatorservice \
		./coordinatorservice/coordinator_service.proto

protos-token:
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		--proto_path=. \
		--proto_path=./token \
		./token/token.proto

protos-blocktx:
	@./scripts/compile_proto.sh

protos-wgclient:
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		--proto_path=. \
		--proto_path=./token \
		--proto_path=./coordinatorservice \
		./wgclient/workload/expected_results.proto

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

build-test-node-image:
	${docker_cmd} build -f $(dockerfile_test_node_dir)/Dockerfile \
	  -t ${image_namespace}/committer-test-node:${version} \
		--build-arg GO_IMAGE=${golang_image} \
		--build-arg DB_IMAGE=${db_image} \
	  .

build-release-images:
	./scripts/build-release-images.sh \
		$(docker_cmd) $(version) $(image_namespace) $(dockerfile_release_dir) $(multiplatform) $(golang_image)

build-mock-orderer-image:
	${docker_cmd} build -f ${dockerfile_release_dir}/Dockerfile \
		-t ${image_namespace}/mock-ordering-service:${version} \
		--build-arg SERVICE_NAME=mockorderingservice \
		--build-arg PORTS=4001 \
		.

.PHONY: lint
lint:
	@echo "Running Go Linters..."
	golangci-lint run --color=always --sort-results --new-from-rev=main --timeout=4m
	@echo "Linting Complete. Parsing Errors..."
