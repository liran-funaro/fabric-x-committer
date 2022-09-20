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
)

type TopLevelConfig struct {
	Logging    LoggingConfig    `mapstructure:"logging"`
	Prometheus PrometheusConfig `mapstructure:"prometheus"`

	ShardsService   ShardConfig    `mapstructure:"shards-service"`
	SigVerification VerifierConfig `mapstructure:"sig-verification"`
}

type ShardConfig struct{}

type VerifierConfig struct{}

type LoggingLevel = string

const (
	Debug LoggingLevel = "DEBUG"
	Info               = "INFO"
	Error              = "ERROR"
)

type LoggingConfig struct {
	Enabled     bool
	Level       LoggingLevel
	Caller      bool
	Development bool
}
type PrometheusConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

var AppConfig *TopLevelConfig

var configUpdateListener []func()

func init() {
	utils.Must(initializeConfig())
}

func initializeConfig() error {
	// Default
	configFiles, err := FilePaths(DirPath, regexp.MustCompile(`^config.*\.yaml`))
	if err != nil {
		return err
	}

	// We start reading config files with smaller length (more general, e.g. config.yaml) to the ones that are more specific (e.g. config-sigverification-host1.yaml)
	sort.Slice(configFiles, func(i, j int) bool {
		return len(configFiles[i]) < len(configFiles[j])
	})
	mergedConfigs, err := MergeYamlConfigs(configFiles...)
	if err != nil {
		return err
	}
	viper.SetConfigType("yaml")
	err = viper.ReadConfig(bytes.NewReader(mergedConfigs))
	if err != nil {
		return err
	}

	// Env vars, e.g. SC_LOGGING_ENABLED=false, but not SC_VERBOSE=false (does not work with aliases)
	viper.SetEnvPrefix("SC")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))

	err = configUpdated()
	if err != nil {
		return err
	}

	return nil
}

//ParseFlags does the following:
//* It adds some default flags to the main.go,
//* parses the flags and
//* updates the config according to the flag mapping as described in the bindings.
func ParseFlags(customBindings ...string) {
	if len(customBindings)%2 != 0 {
		panic("arguments must be in pairs")
	}

	defaultFlagBindings := defaultFlags()
	bindings := append(customBindings, defaultFlagBindings...)

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	for i := 0; i < len(bindings); i += 2 {
		flagName := bindings[i]
		configKey := bindings[i+1]
		utils.Must(viper.BindPFlag(configKey, pflag.CommandLine.Lookup(flagName)))
	}

	if err := configUpdated(); err != nil {
		panic(err)
	}
}

func defaultFlags() []string {
	flag.Bool("verbose", viper.GetBool("logging.enabled"), "Turn on verbose mode")

	// Aliases don't work with bindings
	return []string{"verbose", "logging.enabled"}
}

func OnConfigUpdated(listener func()) {
	configUpdateListener = append(configUpdateListener, listener)
}

func configUpdated() error {
	err := viper.Unmarshal(&AppConfig)
	if err != nil {
		return err
	}
	for _, listener := range configUpdateListener {
		listener()
	}

	//_ = viper.WriteConfigAs(filepath.Join(generated.DirPath, "generated-config.yaml"))
	return nil
}
