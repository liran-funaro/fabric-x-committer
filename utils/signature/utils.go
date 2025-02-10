package signature

import (
	"errors"
	"flag"
	"strings"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("sign")

var schemeMap = map[string]Scheme{
	"ECDSA": Ecdsa,
	"NONE":  NoScheme,
	"BLS":   Bls,
	"EDDSA": Eddsa,
}

func SchemeVar(p *Scheme, name string, defaultValue Scheme, usage string) {
	*p = defaultValue
	flag.Func(name, usage, func(input string) error {
		if scheme, ok := schemeMap[strings.ToUpper(input)]; ok {
			*p = scheme
			return nil
		}
		return errors.New("scheme not found")
	})
}
