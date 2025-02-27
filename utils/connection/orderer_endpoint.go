package connection

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type (
	// OrdererEndpoint defines a party's endpoint.
	OrdererEndpoint struct {
		// ID is the concenter's ID (party).
		ID    uint32 `mapstructure:"id" json:"id,omitempty" yaml:"id,omitempty"`
		MspID string `mapstructure:"msp-id" json:"msp-id,omitempty" yaml:"msp-id,omitempty"`
		// API should be broadcast and/or deliver.
		API      []string `mapstructure:"api" json:"api,omitempty" yaml:"api,omitempty"`
		Endpoint `mapstructure:",squash" json:",inline" yaml:",inline"`
	}
)

const (
	// Broadcast support by endpoint.
	Broadcast = "broadcast"
	// Deliver support by endpoint.
	Deliver = "deliver"
)

var (
	ErrorInvalidEndpointKey = errors.New("invalid endpoint key")
	ErrorInvalidEndpoint    = errors.New("invalid endpoint")
)

// String returns a deterministic representation of the endpoint.
func (e *OrdererEndpoint) String() string {
	var output strings.Builder
	output.WriteString("id=")
	output.WriteString(strconv.FormatUint(uint64(e.ID), 10))
	if len(e.MspID) > 0 {
		output.WriteString(",msp-id=")
		output.WriteString(e.MspID)
	}
	for _, api := range e.API {
		output.WriteRune(',')
		output.WriteString(api)
	}
	if len(e.Host) > 0 || e.Port > 0 {
		output.WriteRune(',')
		output.WriteString(e.Host)
		output.WriteRune(':')
		output.WriteString(strconv.FormatInt(int64(e.Port), 10))
	}
	return output.String()
}

// SupportsAPI returns true if this endpoint supports API.
// It also returns true if no APIs are specified, as we cannot know.
func (e *OrdererEndpoint) SupportsAPI(api string) bool {
	return len(e.API) == 0 || slices.Contains(e.API, api)
}

// ParseOrdererEndpoint parses a string according to the following schema order (the first that succeeds).
// Schema 1: JSON
// Schema 2: YAML
// Schema 3: [id=ID,][msp-id=MspID,][broadcast,][deliver,][host=Host,][port=Port,][Host:Port]
func ParseOrdererEndpoint(valueRaw string) (*OrdererEndpoint, error) {
	ret := &OrdererEndpoint{}
	if len(valueRaw) == 0 {
		return ret, nil
	}

	if err := json.Unmarshal([]byte(valueRaw), ret); err == nil {
		return ret, nil
	}
	if err := yaml.Unmarshal([]byte(valueRaw), ret); err == nil {
		return ret, nil
	}

	metaParts := strings.Split(valueRaw, ",")
	for _, item := range metaParts {
		item = strings.TrimSpace(item)
		if item == Broadcast || item == Deliver {
			ret.API = append(ret.API, item)
		} else if i := strings.Index(item, "="); i >= 0 {
			key, value := strings.TrimSpace(item[:i]), strings.TrimSpace(item[i+1:])
			switch key {
			case "id":
				id, err := strconv.ParseUint(value, 10, 32)
				if err != nil {
					return nil, err
				}
				ret.ID = uint32(id)
			case "msp-id":
				ret.MspID = value
			case "host":
				ret.Host = value
			case "port":
				if err := ret.setPort(value); err != nil {
					return nil, err
				}
			default:
				return nil, ErrorInvalidEndpointKey
			}
		} else if i = strings.Index(item, ":"); i >= 0 {
			ret.Host = strings.TrimSpace(item[:i])
			if err := ret.setPort(strings.TrimSpace(item[i+1:])); err != nil {
				return nil, err
			}
		} else {
			return nil, ErrorInvalidEndpoint
		}
	}
	return ret, nil
}

func (e *OrdererEndpoint) setPort(portStr string) error {
	port, err := strconv.ParseInt(portStr, 10, 32)
	if err != nil {
		return err
	}
	e.Port = int(port)
	return nil
}
