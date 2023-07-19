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

project_dir    := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
sc_runner_dir  ?= $(project_dir)/docker/runner
sc_builder_dir ?= $(project_dir)/docker/builder
output_dir     ?= $(project_dir)/bin
cache_dir      ?= $(shell go env GOCACHE)
mod_cache_dir  ?= $(shell go env GOMODCACHE)
golang_image   ?= golang:1.20-bullseye
env            ?= env

# Set this parameter when running docker-builder-run
# E.g., make docker-builder-run cmd="make build-local"
cmd            ?=

# Set these parameters to compile to a specific os/arch
# Eg.g, make build-local os=linux arch=amd64
os             ?=
arch           ?=

ifneq ($(os),)
env += "GOOS=$(os)"
endif
ifneq ($(arch),)
env += "GOARCH=$(arch)"
endif


.PHONY: test clean

#########################
# Quickstart
#########################

test: build
	go test -v ./...

test-cover: build
	go test -v -coverprofile=coverage.profile ./...

cover-report: test-cover
	go tool cover -html=coverage.profile

clean:
	@rm -rf $(output_dir)

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

build: lint $(output_dir)
	$(env) go build -buildvcs=false -o "$(output_dir)/blockgen" ./wgclient/cmd/generator
	$(env) go build -buildvcs=false -o "$(output_dir)/mockcoordinator" ./wgclient/cmd/mockcoordinator
	$(env) go build -buildvcs=false -o "$(output_dir)/coordinator" ./coordinatorservice/cmd/server
	$(env) go build -buildvcs=false -o "$(output_dir)/coordinator_setup" ./coordinatorservice/cmd/setup_helper
	$(env) go build -buildvcs=false -o "$(output_dir)/sigservice" ./sigverification/cmd/server
	$(env) go build -buildvcs=false -o "$(output_dir)/shardsservice" ./shardsservice/cmd/server
	$(env) go build -buildvcs=false -o "$(output_dir)/sidecar" ./sidecar/cmd/server
	$(env) go build -buildvcs=false -o "$(output_dir)/sidecarclient" ./wgclient/cmd/sidecarclient

build-docker: $(output_dir)
	make docker-builder-run cmd="make build output_dir=$(output_dir)"
	scripts/amend-permissions.sh "$(output_dir)"

# Executes a command from within the docker image.
# Requires that docker-builder-image be run once before, to ensure the image exists.
docker-builder-run: $(cache_dir) $(mod_cache_dir)
	@docker run --rm -it \
	  --mount "type=bind,source=$(project_dir),target=$(project_dir)" \
	  --mount "type=bind,source=$(cache_dir),target=$(cache_dir)" \
	  --mount "type=bind,source=$(mod_cache_dir),target=$(mod_cache_dir)" \
	  --workdir $(project_dir) \
	  --env GOCACHE="$(cache_dir)" \
	  --env GOMODCACHE="$(mod_cache_dir)" \
	  $(golang_image) \
	  $(cmd)
	scripts/amend-permissions.sh "$(cache_dir)" "$(mod_cache_dir)"

# Simple containerized SC
docker-runner-image:
	docker build -f $(sc_runner_dir)/Dockerfile -t sc_runner .


.PHONY: lint
lint:
	@echo "Running Go Linters..."
	golangci-lint run --color=always --sort-results --new-from-rev=main --timeout=2m
	@echo "Linting Complete. Parsing Errors..."
