package cobracmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
)

// CobraInt creates a flag of type integer for the cmd parameter.
func CobraInt(cmd *cobra.Command, flagName, flagUsage, configKey string) {
	cmd.PersistentFlags().Int(flagName, viper.GetInt(configKey), flagUsage)
	utils.Must(viper.BindPFlag(configKey, cmd.PersistentFlags().Lookup(flagName)))
}

// CobraString creates a flag of type string for the cmd parameter.
func CobraString(cmd *cobra.Command, flagName, flagUsage, configKey string) {
	cmd.PersistentFlags().String(flagName, viper.GetString(configKey), flagUsage)
	utils.Must(viper.BindPFlag(configKey, cmd.PersistentFlags().Lookup(flagName)))
}

// CobraBool creates a flag of type boolean for the cmd parameter.
func CobraBool(cmd *cobra.Command, flagName, flagUsage, configKey string) {
	cmd.PersistentFlags().Bool(flagName, viper.GetBool(configKey), flagUsage)
	utils.Must(viper.BindPFlag(configKey, cmd.PersistentFlags().Lookup(flagName)))
}

// CobraDuration creates a flag of type Duration for the cmd parameter.
func CobraDuration(cmd *cobra.Command, flagName, flagUsage, configKey string) {
	cmd.PersistentFlags().Duration(flagName, viper.GetDuration(configKey), flagUsage)
	utils.Must(viper.BindPFlag(configKey, cmd.PersistentFlags().Lookup(flagName)))
}

// SetDefaultFlags setting useful Cobra flags for the cmd parameter.
func SetDefaultFlags(cmd *cobra.Command, serviceName string, configPath *string) {
	cmd.PersistentFlags().StringVar(configPath, "configs", "", "set the absolute path of config directory")

	CobraString(
		cmd,
		"endpoint",
		"Determine the endpoint of the server",
		fmt.Sprintf("%v.server.endpoint", serviceName),
	)

	CobraString(
		cmd,
		"metrics-endpoint",
		"Where prometheus listens for incoming connections",
		fmt.Sprintf("%v.monitoring.metrics.endpoint", serviceName),
	)

	CobraBool(
		cmd,
		"verbose",
		"Turn on verbose mode",
		"logging.enabled",
	)
}

// WaitUntilServiceDone blocks until the service done and classifying the cause of termination.
func WaitUntilServiceDone(ctx context.Context) error {
	<-ctx.Done()
	err := context.Cause(ctx)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

// VersionCmd creates a version command.
func VersionCmd(serviceName, serviceVersion string) *cobra.Command {
	return &cobra.Command{
		Use:          "version",
		Short:        fmt.Sprintf("print the version of the %v service.", serviceName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Printf("%v %v\n", serviceName, serviceVersion)
			return nil
		},
	}
}

// ReadYaml reading the Yaml config file of the service.
func ReadYaml(configPath string) error {
	if configPath == "" {
		return errors.New("--configs flag must be set to the path of the configuration file")
	}
	return config.ReadYamlConfigs([]string{configPath})
}
