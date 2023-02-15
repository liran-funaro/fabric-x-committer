package main

import (
	"github.com/spf13/pflag"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/generatetopology"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

func main() {
	var out = pflag.String("out-dir", "./generatetopology/tmp/nwogenerator/", "Generated output path")
	config.ParseFlags()

	c := generatetopology.ReadConfig()

	utils.Must(generatetopology.Generate(c.Fsc, *out))
}
