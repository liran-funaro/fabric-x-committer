package config

const GRPC_PORT = 5000

type Config struct {
	SigVerifierServers        []string
	ShardsServersAndNumShards map[string]int
}
