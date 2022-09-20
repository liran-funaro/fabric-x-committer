package main

import (
	"fmt"
	"os"

	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/cmd/generator/command"
)

func main() {
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
