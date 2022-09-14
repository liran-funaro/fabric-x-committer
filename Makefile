ROOT_PATH := $(shell pwd)

build:
	./scripts/build_all.sh

run:
	./scripts/copy_and_run.sh

start: build run

.PHONY: sigverification-benchmarks
sigverification-benchmarks: export SC_DEPLOYMENT_ENV=PERF
sigverification-benchmarks:
	for folder in $(dir $(wildcard $(ROOT_PATH)/sigverification/*/)); do \
            echo "Running benchmarks in " $$folder ; \
            cd $$folder ; \
            go test -bench . -benchmem -benchtime=5s -cpu=1 ; \
    done
