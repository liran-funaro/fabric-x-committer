package connection

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Host = string

type Endpoint struct {
	Host Host `mapstructure:"host" json:"host,omitempty" yaml:"host,omitempty"`
	Port int  `mapstructure:"port" json:"port,omitempty" yaml:"port,omitempty"`
}

func (e *Endpoint) Empty() bool {
	return e.Port == 0
}

func (e *Endpoint) Address() string {
	return fmt.Sprintf("%s:%d", e.Host, e.Port)
}

func (e *Endpoint) String() string {
	return e.Address()
}

// CreateEndpoint parses an endpoint from an address string.
// It panics if it fails to parse.
func CreateEndpoint(value string) *Endpoint {
	endpoint, err := NewEndpoint(value)
	if err != nil {
		panic(err)
	}
	return endpoint
}

// NewEndpoint parses an endpoint from an address string.
func NewEndpoint(value string) (*Endpoint, error) {
	if len(value) == 0 {
		return &Endpoint{}, nil
	}
	vals := strings.Split(value, ":")
	if len(vals) != 2 {
		return nil, errors.New("not in format: 1.2.3.4:5")
	}
	host := vals[0]
	port, err := strconv.Atoi(vals[1])
	if err != nil {
		return nil, err
	}
	return &Endpoint{Host: host, Port: port}, nil
}

// NewLocalHost returns a default endpoint "localhost:0".
func NewLocalHost() *Endpoint {
	return &Endpoint{Host: "localhost"}
}
