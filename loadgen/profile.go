package loadgen

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Profile describes the generated workload characteristics.
type Profile struct {
	// Name and Description
	Name        string `yaml:"name"`
	Description string `yaml:"description"`

	Block       BlockProfile       `yaml:"block"`
	Transaction TransactionProfile `yaml:"transaction"`
	Conflicts   ConflictProfile    `yaml:"conflicts"`

	// The number of workers to generate transactions
	TxGenWorkers          uint32 `yaml:"tx-gen-workers"`
	TxSignWorkers         uint32 `yaml:"tx-sign-workers"`
	TxDependenciesWorkers uint32 `yaml:"tx-dependencies-workers"`

	// The seed to generate the seeds for each worker
	Seed int64 `yaml:"seed"`
}

// BlockProfile describes generate block characteristics.
type BlockProfile struct {
	// Size of the block
	Size int64 `yaml:"size"`

	// The queue buffer size
	BufferSize uint32 `yaml:"buffer-size"`
}

// TransactionProfile describes generate TX characteristics.
type TransactionProfile struct {
	// The size of the key to generate
	KeySize             uint32 `yaml:"key-size"`
	BlindWriteValueSize uint32 `yaml:"blind-write-value-size"`
	// The number of keys to generate (read ver=nil)
	ReadOnlyCount *Distribution `yaml:"read-only-count"`
	// The number of keys to generate (read ver=nil/write)
	ReadWriteCount *Distribution `yaml:"read-write-count"`
	// The number of keys to generate (write)
	BlindWriteCount *Distribution `yaml:"write-count"`
	// The queue buffer size
	BufferSize uint32           `yaml:"buffer-size"`
	Signature  SignatureProfile `yaml:"signature"`
}

// ConflictProfile describes the TX conflict characteristics.
// Note that each of the conflicts' probabilities are independent bernoulli distributions.
type ConflictProfile struct {
	// Probability of invalid signatures [0,1] (default: 0)
	InvalidSignatures Probability `yaml:"invalid-signatures"`
	// Dependencies list of dependencies
	Dependencies []DependencyDescription `yaml:"dependencies"`
}

// DependencyDescription describes a dependency type.
type DependencyDescription struct {
	// Probability of the dependency type [0,1] (default: 0)
	Probability Probability `yaml:"probability"`
	// Gap is the distance between the dependent TXs (default: 1)
	Gap *Distribution `yaml:"gap"`
	// Src dependency "read", "write", or "read-write"
	Src string `yaml:"src"`
	// Dst dependency "read", "write", or "read-write"
	Dst string `yaml:"dst"`
}

// SignatureProfile describes how to sign/verify a TX.
type SignatureProfile struct {
	Scheme Scheme `yaml:"scheme"`
	// KeyPath describes how to find/generate the signature keys.
	// KeyPath is still not supported.
	KeyPath *KeyPath `yaml:"key-path"`
}

// KeyPath describes how to find/generate the signature keys.
type KeyPath struct {
	SigningKey      string `yaml:"signing-key"`
	VerificationKey string `yaml:"verification-key"`
	SignCertificate string `yaml:"sign-certificate"`
}

// LoadProfileFromYaml returns a profile loaded from a file.
func LoadProfileFromYaml(yamlPath string) *Profile {
	yamlFile := ReadFileIfExists(yamlPath)
	if yamlFile == nil {
		panic("profile file was not found")
	}

	profile := &Profile{}
	Must(yaml.Unmarshal(yamlFile, profile))
	return profile
}

// PrintProfile prints a profile.
func PrintProfile(profile *Profile) {
	d, err := yaml.Marshal(profile)
	Must(err)
	fmt.Println("############################################################")
	fmt.Println("# Profile")
	fmt.Println("############################################################")
	fmt.Println(string(d))
	fmt.Println("############################################################")
}
