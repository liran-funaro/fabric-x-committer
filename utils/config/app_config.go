package config

import (
	"bytes"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
)

var configUpdateListener []func()

// OnConfigUpdated subscribes a listener whenever a change in the config takes place.
// The config reference will be updated (see Unmarshal), but if further actions are needed, this method can be used.
func OnConfigUpdated(listener func()) {
	configUpdateListener = append(configUpdateListener, listener)
}

func Unmarshal(c interface{}, decoderFuncs ...DecoderFunc) {
	utils.Must(viper.Unmarshal(c, decoderHook(append(decoderFuncs, durationDecoder, endpointDecoder)...)))
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

func defaultConfigFiles() []string {
	configFiles, err := FilePaths(".", regexp.MustCompile(`^config.*\.yaml`))
	if err != nil {
		panic(err)
	}

	// We start reading config files with smaller length (more general, e.g. config.yaml) to the ones that are more specific (e.g. config-sigverification-host1.yaml)
	sort.Slice(configFiles, func(i, j int) bool {
		return len(configFiles[i]) < len(configFiles[j])
	})
	return configFiles
}

func ReadYamlConfigs(configFiles []string) error {
	mergedConfigs, err := MergeYamlConfigs(configFiles...)
	if err != nil {
		return err
	}
	viper.SetConfigType("yaml")
	err = viper.ReadConfig(bytes.NewReader(mergedConfigs))
	if err != nil {
		return err
	}

	return configUpdated()
}
