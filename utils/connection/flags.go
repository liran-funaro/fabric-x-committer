package connection

import (
	"flag"
	"strings"
)

const flagSliceSeparator = ","

func EndpointVars(p *[]*Endpoint, name string, defaultValue []*Endpoint, usage string) {
	*p = defaultValue
	flag.Func(name, usage, func(input string) error {
		endpoints := strings.Split(input, flagSliceSeparator)
		results := make([]*Endpoint, len(endpoints))
		for i, endpoint := range endpoints {
			result, err := NewEndpoint(endpoint)
			if err != nil {
				return err
			}
			results[i] = result
		}
		*p = results
		return nil
	})
}

func SliceFlag(name string, defaultValue []string, usage string) *[]string {
	var p []string
	SliceFlagVar(&p, name, defaultValue, usage)
	return &p
}

func SliceFlagVar(p *[]string, name string, defaultValue []string, usage string) {
	*p = defaultValue
	flag.Func(name, usage, func(slice string) error {
		result := strings.Split(slice, flagSliceSeparator)
		*p = result
		return nil
	})
}
