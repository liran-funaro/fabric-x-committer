package monitoring

type ComponentType = int

const (
	Coordinator ComponentType = iota
	SigVerifier
	ShardsService
)
