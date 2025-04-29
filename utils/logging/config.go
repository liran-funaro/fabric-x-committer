package logging

// Config represents the logging config.
type Config struct {
	Enabled     bool   `mapstructure:"enabled"`
	Level       string `mapstructure:"level"`
	Caller      bool   `mapstructure:"caller"`
	Development bool   `mapstructure:"development"`
	Output      string `mapstructure:"output"`
	Name        string `mapstructure:"name"`
}

// Log levels.
const (
	Debug   string = "DEBUG"
	Info    string = "INFO"
	Warning string = "WARNING"
	Error   string = "ERROR"
)

var defaultConfig = &Config{
	Enabled:     true,
	Level:       Info,
	Caller:      true,
	Development: true,
}
