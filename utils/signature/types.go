package signature

type Message = []byte
type Signature = []byte
type PrivateKey = []byte
type PublicKey = []byte

type Scheme = string

const (
	NoScheme Scheme = "NONE"
	Ecdsa           = "ECDSA"
	Bls             = "BLS"
	Eddsa           = "EDDSA"
)
