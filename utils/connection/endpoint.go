package connection

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

type Host = string

type Endpoint struct {
	Host Host `mapstructure:"host"`
	Port int  `mapstructure:"port"`
}

const endpointSplitter = ":"

func (e *Endpoint) Empty() bool {
	return e.Port == 0
}

func (e *Endpoint) Address() string {
	return fmt.Sprintf("%s%s%d", e.Host, endpointSplitter, e.Port)
}

func (e *Endpoint) String() string {
	return e.Address()
}

func CreateEndpoint(value string) *Endpoint {
	endpoint, err := NewEndpoint(value)
	if err != nil {
		panic(err)
	}
	return endpoint
}

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
