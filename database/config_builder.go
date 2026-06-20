package database

type ConfigBuilder struct {
	config *Config
}

func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		config: NewConfig(),
	}
}

func (cb *ConfigBuilder) DSN(dsn string) *ConfigBuilder {
	cb.config.DSN = dsn
	return cb
}

func (cb *ConfigBuilder) MaxIdleConns(maxIdleConns int) *ConfigBuilder {
	cb.config.MaxIdleConns = maxIdleConns
	return cb
}

func (cb *ConfigBuilder) MaxOpenConns(maxOpenConns int) *ConfigBuilder {
	cb.config.MaxOpenConns = maxOpenConns
	return cb
}

func (cb *ConfigBuilder) ConnMaxLifetime(connMaxLifetime int) *ConfigBuilder {
	cb.config.ConnMaxLifetime = connMaxLifetime
	return cb
}

func (cb *ConfigBuilder) AutoMigrate(autoMigrate bool) *ConfigBuilder {
	cb.config.AutoMigrate = autoMigrate
	return cb
}

func (cb *ConfigBuilder) ShowSQL(showSql bool) *ConfigBuilder {
	cb.config.ShowSql = showSql
	return cb
}

func (cb *ConfigBuilder) Build() *Config {
	return cb.config
}
