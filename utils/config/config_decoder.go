package config

import (
	"reflect"
	"time"

	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type DecoderFunc = func(dataType reflect.Type, targetType reflect.Type, rawData interface{}) (interface{}, bool, error)

// decoderHook contains custom unmarshalling for types not supported by default by mapstructure, e.g. time.Duration, connection.Endpoint
func decoderHook(hooks ...DecoderFunc) viper.DecoderConfigOption {
	return viper.DecodeHook(func(dataType reflect.Type, targetType reflect.Type, rawData interface{}) (interface{}, error) {
		for _, hook := range hooks {
			if result, done, err := hook(dataType, targetType, rawData); done {
				return result, err
			}
		}
		return rawData, nil
	})
}

func durationDecoder(dataType reflect.Type, targetType reflect.Type, rawData interface{}) (interface{}, bool, error) {
	if targetType.Kind() != reflect.Int64 {
		return rawData, false, nil
	}
	if dataType.Kind() != reflect.String {
		return rawData, false, nil
	}
	duration, err := time.ParseDuration(rawData.(string))
	if err != nil {
		return nil, true, err
	}
	return duration, true, nil
}

func endpointDecoder(dataType reflect.Type, targetType reflect.Type, rawData interface{}) (interface{}, bool, error) {
	if targetType != reflect.TypeOf(connection.Endpoint{}) {
		return rawData, false, nil
	}
	if dataType.Kind() != reflect.String {
		return rawData, false, nil
	}
	endpoint, err := connection.NewEndpoint(rawData.(string))
	if err != nil {
		return nil, true, err
	}
	return endpoint, true, nil
}
