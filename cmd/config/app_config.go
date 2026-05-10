/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/go-playground/validator/v10"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/spf13/viper"

	"github.com/hyperledger/fabric-x-committer/loadgen"
	"github.com/hyperledger/fabric-x-committer/mock"
	"github.com/hyperledger/fabric-x-committer/service/coordinator"
	"github.com/hyperledger/fabric-x-committer/service/query"
	"github.com/hyperledger/fabric-x-committer/service/sidecar"
	"github.com/hyperledger/fabric-x-committer/service/vc"
	"github.com/hyperledger/fabric-x-committer/service/verifier"
	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/serve"
)

type loggincConfig struct {
	Logging flogging.Config `mapstructure:"logging"`
}

var (
	logger   = flogging.MustGetLogger("config-reader")
	validate = validator.New()
)

// ReadSidecarYamlAndSetupLogging reading the YAML config file of the sidecar.
func ReadSidecarYamlAndSetupLogging(
	v *viper.Viper, configPath string,
) (*sidecar.Config, *serve.Config, error) {
	return ReadYamlAndSetupLogging[sidecar.Config](v, configPath, "SIDECAR")
}

// ReadCoordinatorYamlAndSetupLogging reading the YAML config file of the coordinator.
func ReadCoordinatorYamlAndSetupLogging(
	v *viper.Viper, configPath string,
) (*coordinator.Config, *serve.Config, error) {
	return ReadYamlAndSetupLogging[coordinator.Config](v, configPath, "COORDINATOR")
}

// ReadVCYamlAndSetupLogging reading the YAML config file of the VC.
func ReadVCYamlAndSetupLogging(v *viper.Viper, configPath string) (*vc.Config, *serve.Config, error) {
	return ReadYamlAndSetupLogging[vc.Config](v, configPath, "VC")
}

// ReadVerifierYamlAndSetupLogging reading the YAML config file of the verifier.
func ReadVerifierYamlAndSetupLogging(
	v *viper.Viper, configPath string,
) (*verifier.Config, *serve.Config, error) {
	return ReadYamlAndSetupLogging[verifier.Config](v, configPath, "VERIFIER")
}

// ReadQueryYamlAndSetupLogging reading the YAML config file of the query service.
func ReadQueryYamlAndSetupLogging(v *viper.Viper, configPath string) (*query.Config, *serve.Config, error) {
	return ReadYamlAndSetupLogging[query.Config](v, configPath, "QUERY")
}

// ReadMockOrdererYamlAndSetupLogging reading the YAML config file of the mock ordering service.
func ReadMockOrdererYamlAndSetupLogging(
	v *viper.Viper, configPath string,
) (*mock.OrdererConfig, *serve.Config, error) {
	return ReadYamlAndSetupLogging[mock.OrdererConfig](v, configPath, "ORDERER")
}

// ReadLoadGenYamlAndSetupLogging reading the YAML config file of the load generator.
func ReadLoadGenYamlAndSetupLogging(
	v *viper.Viper, configPath string,
) (*loadgen.ClientConfig, *serve.Config, error) {
	return ReadYamlAndSetupLogging[loadgen.ClientConfig](v, configPath, "LOADGEN")
}

// ReadYamlAndSetupLogging reading the YAML config file of a service.
// It parses the server configuration separately from the service-specific configuration,
// following the same pattern as logging configuration.
func ReadYamlAndSetupLogging[T any](v *viper.Viper, configPath, serviceName string) (*T, *serve.Config, error) {
	if configPath == "" {
		return nil, nil, errors.New("--config flag must be set")
	}
	content, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to read config file: %s", configPath)
	}
	if err = readYamlConfigsFromIO(v, bytes.NewReader(content)); err != nil {
		return nil, nil, err
	}

	// Parse logging and server configuration separately.
	var loggingWrapper loggincConfig
	var serverConfig serve.Config
	c := new(T)
	if err = unmarshal(v, serviceName, &loggingWrapper, &serverConfig, c); err != nil {
		return nil, nil, err
	}
	flogging.Init(loggingWrapper.Logging)

	return c, &serverConfig, nil
}

// unmarshal populate multiple config objects and validates them.
// It automatically set configuration via environment variables.
// E.g. SC_LOGGING_ENABLED=false, but not SC_VERBOSE=false (does not work with aliases).
func unmarshal(v *viper.Viper, serviceName string, items ...any) error {
	ve := &envVarsSetter{
		v:           v,
		envPrefix:   "SC_" + strings.ToUpper(serviceName),
		envReplacer: strings.NewReplacer("-", "_", ".", "_"),
	}
	for _, c := range items {
		ve.setEnvVars(reflect.TypeOf(c))
	}

	decoders := decoderHook()
	for _, c := range items {
		if err := v.Unmarshal(c, decoders); err != nil {
			return errors.Wrap(err, "error decoding config")
		}
		logger.Debugf("Decoded config: %s", &utils.LazyJSON{O: c})
		if err := validate.Struct(c); err != nil {
			return errors.Wrap(err, "error validating config")
		}
	}
	return nil
}

// readYamlConfigsFromIO reads configurations from IO.
func readYamlConfigsFromIO(v *viper.Viper, in io.Reader) error {
	v.SetConfigType("yaml")
	return errors.Wrap(v.ReadConfig(in), "failed to read config")
}
