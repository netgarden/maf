package logging

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/netgarden/maf"
)

func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager *maf.Manager
	config  *maf.Config
}

func (m *Module) GetID() string {
	return "logging"
}

func (m *Module) GetName() string {
	return "logging"
}

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

func (m *Module) GetConfigSchema() []maf.ConfigItem {
	return []maf.ConfigItem{
		{Name: "logging.level", Type: maf.String, DefaultValue: "debug"},
		{Name: "logging.format", Type: maf.String, DefaultValue: "text"},
		{Name: "logging.destination", Type: maf.String, DefaultValue: "stdout"},
		{Name: "logging.destinationFilepath", Type: maf.String},
	}
}

func (m *Module) SetConfig(config *maf.Config) {
	m.config = config
}

func (m *Module) SetupLogging() error {

	cfg := m.config.Sub("logging")
	level := cfg.GetString("level")
	format := cfg.GetString("format")
	destination := cfg.GetString("destination")
	//destinationFilepath := cfg.GetString("destinationFilepath")

	var w io.Writer
	var slogLevel slog.Level
	var handler slog.Handler

	if strings.EqualFold("stdout", destination) {
		w = os.Stdout
	} else {
		return errors.New("Unknown logging destination: " + destination)
	}

	if strings.EqualFold("debug", level) {
		slogLevel = slog.LevelDebug
	} else if strings.EqualFold("info", level) {
		slogLevel = slog.LevelInfo
	} else if strings.EqualFold("warn", level) {
		slogLevel = slog.LevelWarn
	} else if strings.EqualFold("error", level) {
		slogLevel = slog.LevelError
	} else {
		return errors.New("Unknown logging level: " + level)
	}

	options := &slog.HandlerOptions{
		Level: slogLevel,
	}

	if strings.EqualFold("text", format) {
		handler = slog.NewTextHandler(w, options)
	} else if strings.EqualFold("json", format) {
		handler = slog.NewJSONHandler(w, options)
	} else {
		return errors.New("Unknown logging format: " + format)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	return nil
}
