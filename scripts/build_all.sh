#!/bin/bash

rootDir="scalable-committer"

workingDir="$(basename "$(pwd)")"
while [ "$workingDir" != "$rootDir" ]
do
    cd ..
    workingDir="$(basename "$(pwd)")"
done

rootDir="$(pwd)"

binDir="${rootDir}/bin"
COORDINATOR_MAIN="${rootDir}/pipeline/cmd/server/main.go"
COORDINATOR_BIN="coordinator"
SHARDSSERVICE_MAIN="${rootDir}/shardsservice/cmd/server/main.go"
SHARDSSERVICE_BIN="shardsservice"
SIGVERIFICATION_MAIN="${rootDir}/sigverification/cmd/server/main.go"
SIGVERIFICATION_BIN="sigservice"

mkdir -p ${binDir}

go build -o "${binDir}/${COORDINATOR_BIN}" ${COORDINATOR_MAIN}
go build -o "${binDir}/${SHARDSSERVICE_BIN}" ${SHARDSSERVICE_MAIN}
go build -o "${binDir}/${SIGVERIFICATION_BIN}" ${SIGVERIFICATION_MAIN}