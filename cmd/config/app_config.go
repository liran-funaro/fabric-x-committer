package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/viper"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("config reader")

var configUpdateListener []func()

// Unmarshal populate a config object.
func Unmarshal(c any) {
	utils.Must(viper.Unmarshal(c, decoderHook(decoders...)))
	logger.Debugf("Decoded config: %s", &utils.LazyJSON{O: c})
}

// configUpdated updates all listeners and pointers to configurations.
// It is called whenever a change in the config takes place (e.g. init, flags set, manual change).
// An alternative solution would be the built-in viper.WatchConfig, but that would require an extra go routine.
func configUpdated() error {
	for _, listener := range configUpdateListener {
		listener()
	}

	return nil
}

func init() {
	// register logging for config updates
	configUpdateListener = append(configUpdateListener, initializeLoggerViaConfig)
	utils.Must(initializeConfig())
}

func initializeConfig() error {
	// Env vars, e.g. SC_LOGGING_ENABLED=false, but not SC_VERBOSE=false (does not work with aliases)
	viper.SetEnvPrefix("SC")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	return configUpdated()
}

// ReadYamlConfigFile reads configurations from files.
func ReadYamlConfigFile(configFile string) error {
	content, err := os.ReadFile(filepath.Clean(configFile))
	if err != nil {
		return errors.Wrapf(err, "failed to read config file: %s", configFile)
	}
	return ReadYamlConfigsFromIO(bytes.NewReader(content))
}

// LoadYamlConfigs loads configurations from string.
func LoadYamlConfigs(config string) error {
	return ReadYamlConfigsFromIO(bytes.NewBufferString(config))
}

// ReadYamlConfigsFromIO reads configurations from IO.
func ReadYamlConfigsFromIO(in io.Reader) error {
	viper.SetConfigType("yaml")
	err := viper.ReadConfig(in)
	if err != nil {
		return errors.Wrap(err, "failed to read config")
	}
	return configUpdated()
}
