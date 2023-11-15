package logging

type Level = string

const (
	Debug   Level = "DEBUG"
	Info          = "INFO"
	Warning       = "WARNING"
	Error         = "ERROR"
)

type Config struct {
	Enabled     bool   `mapstructure:"enabled"`
	Level       Level  `mapstructure:"level"`
	Caller      bool   `mapstructure:"caller"`
	Development bool   `mapstructure:"development"`
	Output      string `mapstructure:"output"`
}

var defaultConfig = &Config{
	Enabled:     true,
	Level:       Info,
	Caller:      false,
	Development: true,
}
