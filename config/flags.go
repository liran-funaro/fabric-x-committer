package config

import (
	"flag"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

//ParseFlags parses the flags and adds some default flags to the main.go,
func ParseFlags() {
	Bool("verbose", "logging.enabled", "Turn on verbose mode")
	configPaths := connection.SliceFlag("configs", []string{}, "Config file paths to load")
	pflag.CommandLine.AddGoFlag(flag.CommandLine.Lookup("configs"))

	pflag.Parse()

	if len(*configPaths) > 0 {
		utils.Must(readYamlConfigs(*configPaths))
	}

	if err := configUpdated(); err != nil {
		panic(err)
	}
}

func ServerConfig(component string) {
	String("server", component+".endpoint", "Where the server listens for incoming connections")
	Bool("prometheus-enabled", component+".prometheus.enabled", "Enable prometheus metrics to be kept")
	String("prometheus-endpoint", component+".prometheus.endpoint", "Where prometheus listens for incoming connections")
}

func String(name, configKey, usage string) *string {
	p := flag.String(name, viper.GetString(configKey), usage)
	bindFlag(configKey, name)
	return p
}

func Bool(name, configKey, usage string) *bool {
	p := flag.Bool(name, viper.GetBool(configKey), usage)
	bindFlag(configKey, name)
	return p
}

func Duration(name, configKey, usage string) *time.Duration {
	p := flag.Duration(name, viper.GetDuration(configKey), usage)
	bindFlag(configKey, name)
	return p
}

func Int(name, configKey, usage string) *int {
	p := flag.Int(name, viper.GetInt(configKey), usage)
	bindFlag(configKey, name)
	return p
}

func bindFlag(configKey, name string) {
	pflag.CommandLine.AddGoFlag(flag.CommandLine.Lookup(name))
	utils.Must(viper.BindPFlag(configKey, pflag.CommandLine.Lookup(name)))
}
