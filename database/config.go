package database

type Config struct {
	DSN             string `yaml:"dsn" envconfig:"DSN"`
	MaxIdleConns    int    `yaml:"maxIdleConns" envconfig:"MAX_IDLE_CONNS"`
	MaxOpenConns    int    `yaml:"maxOpenConns" envconfig:"MAX_OPEN_CONNS"`
	ConnMaxLifetime int    `yaml:"connMaxLifetime" envconfig:"CONNS_MAX_LIFE_TIME"`
	AutoMigrate     bool   `yaml:"autoMigrate" envconfig:"AUTO_MIGRATE"`
	ShowSql         bool   `yaml:"showSql" envconfig:"SHOW_SQL"`
}

func NewConfig() *Config {
	return &Config{
		DSN:             "host=localhost user=app password=app dbname=app port=5432 sslmode=disable",
		MaxIdleConns:    5,
		MaxOpenConns:    10,
		ConnMaxLifetime: 3600,
		AutoMigrate:     false,
		ShowSql:         false,
	}
}
