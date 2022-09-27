package config

import (
	"bytes"
	"flag"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

var configUpdateListener []func()

var configUpdaters []func()

//Unmarshal is called within the init method of a part of the configuration,
//in order to read the viper config into the object.
//It will automatically update the reference upon every config update
func Unmarshal(c interface{}) {
	updater := func() {
		utils.Must(viper.Unmarshal(c, decoder))
	}
	updater()
	configUpdaters = append(configUpdaters, updater)
}

//OnConfigUpdated subscribes a listener whenever a change in the config takes place.
//The config reference will be updated (see Unmarshal), but if further actions are needed, this method can be used.
func OnConfigUpdated(listener func()) {
	configUpdateListener = append(configUpdateListener, listener)
}

//configUpdated updates all listeners and pointers to configurations.
//It is called whenever a change in the config takes place (e.g. init, flags set, manual change).
//An alternative solution would be the built-in viper.WatchConfig, but that would require an extra go routine.
func configUpdated() error {
	for _, listener := range configUpdateListener {
		listener()
	}
	for _, updater := range configUpdaters {
		updater()
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
	configFiles, err := FilePaths(DirPath, regexp.MustCompile(`^config.*\.yaml`))
	if err != nil {
		panic(err)
	}

	// We start reading config files with smaller length (more general, e.g. config.yaml) to the ones that are more specific (e.g. config-sigverification-host1.yaml)
	sort.Slice(configFiles, func(i, j int) bool {
		return len(configFiles[i]) < len(configFiles[j])
	})
	return configFiles
}

//ParseFlags does the following:
//* It adds some default flags to the main.go,
//* parses the flags and
//* updates the config according to the flag mapping as described in the bindings.
func ParseFlags(customBindings ...string) {
	if len(customBindings)%2 != 0 {
		panic("arguments must be in pairs")
	}

	// --verbose flag
	flag.Bool("verbose", viper.GetBool("logging.enabled"), "Turn on verbose mode")
	defaultFlagBindings := []string{"verbose", "logging.enabled"} // Aliases don't work with bindings

	// --configs flag
	configPaths := connection.SliceFlag("configs", []string{}, "Config file paths to load")

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	if len(*configPaths) > 0 {
		utils.Must(readYamlConfigs(*configPaths))
	}

	bindings := append(customBindings, defaultFlagBindings...)
	for i := 0; i < len(bindings); i += 2 {
		flagName := bindings[i]
		configKey := bindings[i+1]
		utils.Must(viper.BindPFlag(configKey, pflag.CommandLine.Lookup(flagName)))
	}

	if err := configUpdated(); err != nil {
		panic(err)
	}
}

//readYamlConfigs reads multiple config files in the order given and puts them together to compose the final config.
//If a config value is defined in multiple files, then the latter ones overwrite the former ones.
func readYamlConfigs(configFiles []string) error {
	mergedConfigs, err := MergeYamlConfigs(configFiles...)
	if err != nil {
		return err
	}
	viper.SetConfigType("yaml")
	err = viper.ReadConfig(bytes.NewReader(mergedConfigs))
	if err != nil {
		return err
	}
	return nil
}
