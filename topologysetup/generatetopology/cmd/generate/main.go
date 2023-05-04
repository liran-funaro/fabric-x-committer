package main

import (
	"github.com/spf13/pflag"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/generatetopology"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/config"
)

func main() {
	var out = pflag.String("out-dir", "./tmp/nwogenerator/", "Generated output path")
	config.ParseFlags()

	c := generatetopology.ReadConfig()

	utils.Must(generatetopology.Generate(c.Fsc, *out))
}
