#!/usr/bin/env just --justfile

build:
	./scripts/build_all.sh

default := ''

run arg=default:
	./scripts/copy_and_run.sh {{arg}}

start arg=default: build (run arg)