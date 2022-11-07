package logging

type Level = string

const (
	Debug Level = "DEBUG"
	Info        = "INFO"
	Error       = "ERROR"
)

type Config struct {
	Enabled     bool
	Level       Level
	Caller      bool
	Development bool
	Output      string
}

var defaultConfig = &Config{
	Enabled:     true,
	Level:       Info,
	Caller:      false,
	Development: true,
}
