package main

import (
	"fmt"
	"os"

	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/cmd/generator/command"
)

// run with GOGC=20000

func main() {
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
