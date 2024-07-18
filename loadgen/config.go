package loadgen

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Profile describes the generated workload characteristics.
// It only contains parameters that deterministically affect the
// generated items.
// The items order, however, might be affected by other parameters.
type Profile struct {
	Block       BlockProfile       `mapstructure:"block"`
	Key         KeyProfile         `mapstructure:"key"`
	Transaction TransactionProfile `mapstructure:"transaction"`
	Query       QueryProfile       `mapstructure:"query"`
	Conflicts   ConflictProfile    `mapstructure:"conflicts"`

	// The seed to generate the seeds for each worker
	Seed int64 `mapstructure:"seed"`

	// Workers is the number of independent producers.
	// Each worker uses a unique seed that is generated from the main seed.
	// To ensure responsibility of items between runs (e.g., for query)
	// the number of workers must be preserved.
	Workers uint32 `mapstructure:"workers"`
}

// KeyProfile describes generated keys characteristics.
type KeyProfile struct {
	// Size is the size of the key to generate.
	Size uint32 `mapstructure:"size"`
}

// BlockProfile describes generate block characteristics.
type BlockProfile struct {
	// Size of the block
	Size int64 `mapstructure:"size"`
}

// TransactionProfile describes generate TX characteristics.
type TransactionProfile struct {
	// The sizes of the values to generate (size=0 => value=nil)
	ReadWriteValueSize  uint32 `mapstructure:"read-write-value-size"`
	BlindWriteValueSize uint32 `mapstructure:"blind-write-value-size"`
	// The number of keys to generate (read ver=nil)
	ReadOnlyCount *Distribution `mapstructure:"read-only-count"`
	// The number of keys to generate (read ver=nil/write)
	ReadWriteCount *Distribution `mapstructure:"read-write-count"`
	// The number of keys to generate (write)
	BlindWriteCount *Distribution    `mapstructure:"write-count"`
	Signature       SignatureProfile `mapstructure:"signature"`
}

// QueryProfile describes generate query characteristics.
type QueryProfile struct {
	// The number of keys to query.
	QuerySize *Distribution `mapstructure:"query-size"`
	// The minimal portion of invalid keys (1 => all keys are invalid).
	// This is a lower bound since some valid keys might have failed to commit due to conflicts.
	MinInvalidKeysPortion *Distribution `mapstructure:"min-invalid-keys-portion"`
	// If Shuffle=false, the valid keys will be placed first.
	// Otherwise, they will be shuffled.
	Shuffle bool `mapstructure:"shuffle"`
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

// StreamOptions allows adjustment to the stream rate.
// It only contains parameters that do not affect the produced items.
// However, these parameters might affect the order of the items.
type StreamOptions struct {
	// RateLimit directly impacts the rate by limiting it.
	RateLimit *LimiterConfig `mapstructure:"rate-limit"`
	// GenBatch impacts the rate by batching generated items before inserting then the channel.
	// This helps overcome the inherit rate limitation of Go channels.
	GenBatch uint32 `mapstructure:"gen-batch"`
	// BuffersSize impact the rate by masking fluctuation in performance.
	BuffersSize int `mapstructure:"buffers-size"`
}

// Debug outputs the profile to stdout.
func (p *Profile) Debug() {
	debug("Profile", p)
}

// Debug outputs the stream configuration to stdout.
func (o *StreamOptions) Debug() {
	debug("Stream Config", o)
}

func debug(title string, val any) {
	d, err := yaml.Marshal(val)
	Must(err)
	fmt.Println("############################################################")
	fmt.Printf("# %s\n", title)
	fmt.Println("############################################################")
	fmt.Println(string(d))
	fmt.Println("############################################################")
}
