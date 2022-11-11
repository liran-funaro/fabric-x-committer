package monitoring

type ComponentType = int

const (
	Coordinator ComponentType = iota
	SigVerifier
	ShardsService
	Generator
)

var componentTypeName = map[ComponentType]string{
	Coordinator:   "Coordinator",
	SigVerifier:   "SigVerifier",
	ShardsService: "ShardsService",
	Generator:     "Generator",
}
