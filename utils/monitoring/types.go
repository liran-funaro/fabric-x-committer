package monitoring

type ComponentType = int

const (
	Coordinator ComponentType = iota
	SigVerifier
	ShardsService
	Generator
	Sidecar
	Other
)

var componentTypeMap = map[ComponentType]string{
	Coordinator:   "coordinator",
	SigVerifier:   "sigverifier",
	ShardsService: "shards-service",
	Generator:     "generator",
	Sidecar:       "sidecar",
	Other:         "other",
}
