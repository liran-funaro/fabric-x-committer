package config

import (
	"bytes"
	"io"
	"strings"

	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("config reader")

var configUpdateListener []func()

// OnConfigUpdated subscribes a listener whenever a change in the config takes place.
// The config reference will be updated (see Unmarshal), but if further actions are needed, this method can be used.
func OnConfigUpdated(listener func()) {
	configUpdateListener = append(configUpdateListener, listener)
}

func Unmarshal(c interface{}, decoderFuncs ...DecoderFunc) {
	utils.Must(viper.Unmarshal(c, decoderHook(append(decoderFuncs, durationDecoder, endpointDecoder)...)))
	logger.Debugf("Decoded config: %v", c)
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
	configUpdateListener = append(configUpdateListener, func() {
		utils.Must(initializeLoggerViaConfig())
	})

	utils.Must(initializeConfig())
}

func initializeConfig() error {
	//// Read config files in the directory by default
	//err := readYamlConfigs(defaultConfigFiles())
	//if err != nil {
	//	return err
	//}

	// Env vars, e.g. SC_LOGGING_ENABLED=false, but not SC_VERBOSE=false (does not work with aliases)
	viper.SetEnvPrefix("SC")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))

	err := configUpdated()
	if err != nil {
		return err
	}

	return nil
}

func ReadYamlConfigs(configFiles []string) error {
	mergedConfigs, err := MergeYamlConfigs(configFiles...)
	if err != nil {
		return err
	}
	return ReadYamlConfigsFromIO(bytes.NewReader(mergedConfigs))
}

func LoadYamlConfigs(config string) error {
	return ReadYamlConfigsFromIO(bytes.NewBufferString(config))
}

func ReadYamlConfigsFromIO(in io.Reader) error {
	viper.SetConfigType("yaml")
	err := viper.ReadConfig(in)
	if err != nil {
		return err
	}
	return configUpdated()
}
