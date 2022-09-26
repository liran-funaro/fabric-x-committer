package logging

type Level = string

const (
	Debug Level = "DEBUG"
	Info        = "INFO"
	Error       = "ERROR"
)

var configWrapper struct {
	Config struct {
		Enabled     bool
		Level       Level
		Caller      bool
		Development bool
	} `mapstructure:"logging"`
}

var Config = &configWrapper.Config
