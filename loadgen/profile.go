package loadgen

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Profile describes the generated workload characteristics.
type Profile struct {
	Block       BlockProfile       `mapstructure:"block"`
	Transaction TransactionProfile `mapstructure:"transaction"`
	Conflicts   ConflictProfile    `mapstructure:"conflicts"`

	// The number of workers to generate transactions
	TxGenWorkers          uint32 `mapstructure:"tx-gen-workers"`
	TxSignWorkers         uint32 `mapstructure:"tx-sign-workers"`
	TxDependenciesWorkers uint32 `mapstructure:"tx-dependencies-workers"`

	// The seed to generate the seeds for each worker
	Seed int64 `mapstructure:"seed"`
}

// BlockProfile describes generate block characteristics.
type BlockProfile struct {
	// Size of the block
	Size int64 `mapstructure:"size"`

	// The queue buffer size
	BufferSize uint32 `mapstructure:"buffer-size"`
}

// TransactionProfile describes generate TX characteristics.
type TransactionProfile struct {
	// The size of the key to generate
	KeySize uint32 `mapstructure:"key-size"`
	// The sizes of the values to generate (size=0 => value=nil)
	ReadWriteValueSize  uint32 `mapstructure:"read-write-value-size"`
	BlindWriteValueSize uint32 `mapstructure:"blind-write-value-size"`
	// The number of keys to generate (read ver=nil)
	ReadOnlyCount *Distribution `mapstructure:"read-only-count"`
	// The number of keys to generate (read ver=nil/write)
	ReadWriteCount *Distribution `mapstructure:"read-write-count"`
	// The number of keys to generate (write)
	BlindWriteCount *Distribution `mapstructure:"write-count"`
	// The queue buffer size
	BufferSize uint32           `mapstructure:"buffer-size"`
	Signature  SignatureProfile `mapstructure:"signature"`
}

// ConflictProfile describes the TX conflict characteristics.
// Note that each of the conflicts' probabilities are independent bernoulli distributions.
type ConflictProfile struct {
	// Probability of invalid signatures [0,1] (default: 0)
	InvalidSignatures Probability `mapstructure:"invalid-signatures"`
	// Dependencies list of dependencies
	Dependencies []DependencyDescription `mapstructure:"dependencies"`
}

// DependencyDescription describes a dependency type.
type DependencyDescription struct {
	// Probability of the dependency type [0,1] (default: 0)
	Probability Probability `mapstructure:"probability"`
	// Gap is the distance between the dependent TXs (default: 1)
	Gap *Distribution `mapstructure:"gap"`
	// Src dependency "read", "write", or "read-write"
	Src string `mapstructure:"src"`
	// Dst dependency "read", "write", or "read-write"
	Dst string `mapstructure:"dst"`
}

// SignatureProfile describes how to sign/verify a TX.
type SignatureProfile struct {
	Scheme Scheme `mapstructure:"scheme"`
	// KeyPath describes how to find/generate the signature keys.
	// KeyPath is still not supported.
	KeyPath *KeyPath `mapstructure:"key-path"`
}

// KeyPath describes how to find/generate the signature keys.
type KeyPath struct {
	SigningKey      string `mapstructure:"signing-key"`
	VerificationKey string `mapstructure:"verification-key"`
	SignCertificate string `mapstructure:"sign-certificate"`
}

func (p *Profile) Debug() {
	d, err := yaml.Marshal(p)
	Must(err)
	fmt.Println("############################################################")
	fmt.Println("# Profile")
	fmt.Println("############################################################")
	fmt.Println(string(d))
	fmt.Println("############################################################")
}
