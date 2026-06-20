package maf

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func NewConfigLoader(manager *Manager) *ConfigLoader {
	return &ConfigLoader{
		manager: manager,
	}
}

type ConfigLoader struct {
	manager *Manager
}

func (cl *ConfigLoader) Load(schema []ConfigItem) (*Config, error) {

	slog.Debug("Loading application configuration")

	// 1. Apply schema defaults.
	values := cl.loadDefaults(schema)

	// 2. Load YAML file (nested keys are flattened to dot-notation).
	yamlValues, err := cl.loadYaml(schema)
	if err != nil {
		return nil, err
	}
	maps.Copy(values, yamlValues)

	// 4. ENV overrides — only keys declared in schema; env always wins.
	envValues, err := cl.loadEnv(schema)
	if err != nil {
		return nil, err
	}
	maps.Copy(values, envValues)

	// 5. Validate required keys.
	err = cl.checkMissing(schema, values)
	if err != nil {
		return nil, err
	}

	return &Config{values: values}, nil
}

func (cl *ConfigLoader) loadDefaults(schema []ConfigItem) map[string]any {

	slog.Debug("Loading default configuration values")

	values := make(map[string]any)

	// 1. Apply schema defaults.
	for _, item := range schema {
		if item.DefaultValue != nil {
			values[item.Name] = item.DefaultValue
		}
	}

	return values
}

func (cl *ConfigLoader) loadYaml(schema []ConfigItem) (map[string]any, error) {

	configFilePath := cl.manager.GetApplication().GetConfigFilePath()
	if configFilePath == "" {
		slog.Debug("No configuration file defined, skipping")
		return make(map[string]any), nil
	}

	slog.Debug("Loading YAML configuration values from " + configFilePath)

	file, err := os.Open(configFilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	raw := make(map[string]any)
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&raw)
	if err != nil {
		return nil, err
	}

	values := make(map[string]any)
	cl.flattenMap("", raw, values)

	err = cl.parseDurations(schema, values)
	if err != nil {
		return nil, err
	}

	return values, nil
}

func (cl *ConfigLoader) loadEnv(schema []ConfigItem) (map[string]any, error) {

	slog.Debug("Loading environment configuration values")

	values := make(map[string]any)

	appID := cl.manager.GetApplication().GetID()

	for _, item := range schema {

		raw, ok := os.LookupEnv(cl.envKey(appID, item.Name))
		if !ok {
			continue
		}

		coerced, err := cl.coerce(raw, item.Type)

		if err != nil {
			return nil, fmt.Errorf("env %s: %w", cl.envKey(appID, item.Name), err)
		}
		values[item.Name] = coerced
	}

	return values, nil
}

// envKey maps "auth.sessionTTL" → "MYAPP_AUTH_SESSIONTTL"
func (cl *ConfigLoader) envKey(appID string, key string) string {
	s := strings.ReplaceAll(key, ".", "_")
	return strings.ToUpper(appID) + "_" + strings.ToUpper(s)
}

func (cl *ConfigLoader) flattenMap(prefix string, src map[string]any, dst map[string]any) {
	for k, v := range src {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if nested, ok := v.(map[string]any); ok {
			cl.flattenMap(key, nested, dst)
		} else {
			dst[key] = v
		}
	}
}

func (cl *ConfigLoader) parseDurations(schema []ConfigItem, values map[string]any) error {

	for _, item := range schema {

		if item.Type != Duration {
			continue
		}

		v, ok := values[item.Name]
		if !ok {
			continue
		}

		if s, isStr := v.(string); isStr {
			d, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("config key %q: invalid duration %q", item.Name, s)
			}
			values[item.Name] = d
		}

	}

	return nil
}

func (cl *ConfigLoader) coerce(s string, t ConfigItemType) (any, error) {
	switch t {
	case String:
		return s, nil
	case Int:
		return strconv.Atoi(s)
	case Float64:
		return strconv.ParseFloat(s, 64)
	case Bool:
		return strconv.ParseBool(s)
	case Duration:
		return time.ParseDuration(s)
	case StringSlice:
		return strings.Split(s, ","), nil
	default:
		return nil, fmt.Errorf("unknown config item type %d", t)
	}
}

func (cl *ConfigLoader) checkMissing(schema []ConfigItem, values map[string]any) error {

	var missing []string

	for _, item := range schema {
		if item.Required {
			if _, ok := values[item.Name]; !ok {
				missing = append(missing, item.Name)
			}
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("required config keys not set: %s", strings.Join(missing, ", "))
	}

	return nil
}
