package maf

import "time"

type Config struct {
	values map[string]any
	prefix string
}

// NewConfig constructs a Config from a flat key→value map. Useful in tests.
func NewConfig(values map[string]any) *Config {
	return &Config{values: values}
}

func (c *Config) fullKey(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + "." + key
}

func (c *Config) GetString(key string) string {
	v, _ := c.values[c.fullKey(key)]
	s, _ := v.(string)
	return s
}

func (c *Config) GetInt(key string) int {
	v, _ := c.values[c.fullKey(key)]
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func (c *Config) GetFloat64(key string) float64 {
	v, _ := c.values[c.fullKey(key)]
	f, _ := v.(float64)
	return f
}

func (c *Config) GetBool(key string) bool {
	v, _ := c.values[c.fullKey(key)]
	b, _ := v.(bool)
	return b
}

func (c *Config) GetDuration(key string) time.Duration {
	v, _ := c.values[c.fullKey(key)]
	d, _ := v.(time.Duration)
	return d
}

func (c *Config) GetStringSlice(key string) []string {
	v, _ := c.values[c.fullKey(key)]
	sl, _ := v.([]string)
	return sl
}

// Sub returns a scoped view of the config.
// cfg.Sub("auth").GetString("sessionTTL") == cfg.GetString("auth.sessionTTL")
func (c *Config) Sub(prefix string) *Config {
	full := prefix
	if c.prefix != "" {
		full = c.prefix + "." + prefix
	}
	return &Config{values: c.values, prefix: full}
}
