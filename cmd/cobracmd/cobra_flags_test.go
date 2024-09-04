package cobracmd

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
)

const configPath = "test-config.yaml"

type TestConfig struct {
	BoolFlag     bool          `mapstructure:"bool-flag"`
	DurationFlag time.Duration `mapstructure:"duration-flag"`
	IntFlag      int           `mapstructure:"int-flag"`
	StringFlag   string        `mapstructure:"string-flag"`
}

func TestReadConfig(t *testing.T) {
	defaultYamlValuesConfig := TestConfig{
		BoolFlag:     false,
		DurationFlag: 1 * time.Second,
		IntFlag:      1,
		StringFlag:   "default-value",
	}

	testCmd := startCmd(t, defaultYamlValuesConfig)
	testCmd.SetArgs([]string{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	_, err := testCmd.ExecuteContextC(ctx)
	require.NoError(t, err)
}

func TestCobraFlags(t *testing.T) {
	expectedConfig := TestConfig{
		BoolFlag:     true,
		DurationFlag: 99 * time.Millisecond,
		IntFlag:      99,
		StringFlag:   "test-string",
	}

	testCmd := startCmd(t, expectedConfig)

	args := []string{
		"--int-flag", strconv.Itoa(expectedConfig.IntFlag),
		"--string-flag", expectedConfig.StringFlag,
		"--bool-flag",
		"--duration-flag", expectedConfig.DurationFlag.String(),
	}

	testCmd.SetArgs(args)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	_, err := testCmd.ExecuteContextC(ctx)
	require.NoError(t, err)
}

func startCmd(t *testing.T, expectedConfig TestConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "Test-Cmd",
		Short: "Test command to check the binding between Cobra and Viper.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := ReadYaml(configPath); err != nil {
				return err
			}
			loadedConfig := readConfig()
			require.Equal(t, expectedConfig, loadedConfig)
			return nil
		},
	}
	setFlags(cmd)
	return cmd
}

func setFlags(cmd *cobra.Command) {
	CobraInt(
		cmd,
		"int-flag",
		"Testing",
		"test-config.int-flag",
	)
	CobraString(
		cmd,
		"string-flag",
		"Testing",
		"test-config.string-flag",
	)
	CobraBool(
		cmd,
		"bool-flag",
		"Testing",
		"test-config.bool-flag",
	)
	CobraDuration(
		cmd,
		"duration-flag",
		"Testing",
		"test-config.duration-flag",
	)
}

func readConfig() TestConfig {
	wrapper := new(struct {
		TestConfig `mapstructure:"test-config"`
	})
	config.Unmarshal(wrapper)
	return wrapper.TestConfig
}
