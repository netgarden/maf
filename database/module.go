package database

import (
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/netgarden/maf"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewModule(configDefaults ...map[string]any) *Module {
	m := &Module{}
	if len(configDefaults) > 0 {
		m.configDefaults = configDefaults[0]
	}
	return m
}

type Module struct {
	manager        *maf.Manager
	appConfig      *maf.Config
	config         *Config
	db             *gorm.DB
	configDefaults map[string]any
}

func (m *Module) GetID() string {
	return "database"
}

func (m *Module) GetName() string {
	return "database"
}

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

func (m *Module) GetConfigSchema() []maf.ConfigItem {
	items := []maf.ConfigItem{
		{Name: "database.dsn", Type: maf.String, DefaultValue: "host=localhost user=app password=app dbname=app port=5432 sslmode=disable"},
		{Name: "database.maxIdleConns", Type: maf.Int, DefaultValue: 5},
		{Name: "database.maxOpenConns", Type: maf.Int, DefaultValue: 10},
		{Name: "database.connMaxLifetime", Type: maf.Int, DefaultValue: 3600},
		{Name: "database.autoMigrate", Type: maf.Bool},
		{Name: "database.showSql", Type: maf.Bool},
	}
	for i, item := range items {
		if v, ok := m.configDefaults[item.Name]; ok {
			items[i].DefaultValue = v
		}
	}
	return items
}

func (m *Module) SetConfig(config *maf.Config) {
	m.appConfig = config
}

func (m *Module) PreInitialize() error {

	cfg := m.appConfig.Sub("database")
	m.config = &Config{
		DSN:             cfg.GetString("dsn"),
		MaxIdleConns:    cfg.GetInt("maxIdleConns"),
		MaxOpenConns:    cfg.GetInt("maxOpenConns"),
		ConnMaxLifetime: cfg.GetInt("connMaxLifetime"),
		AutoMigrate:     cfg.GetBool("autoMigrate"),
		ShowSql:         cfg.GetBool("showSql"),
	}

	var err error

	m.db, err = m.connect()
	if err != nil {
		return err
	}

	err = m.autoMigrate()
	if err != nil {
		return err
	}

	m.notifyConsumers()

	return nil
}

func (m *Module) connect() (*gorm.DB, error) {

	gormPgConfig := postgres.Config{
		DSN: m.config.DSN,
	}

	gormConfig := &gorm.Config{}
	if m.config.ShowSql {
		gormConfig.Logger = logger.Default.LogMode(logger.Info)
	} else {
		// logger.Default has IgnoreRecordNotFoundError: false, and
		// gorm.Open falls back to it whenever Logger is left nil - so
		// callers doing a plain lookup-that-may-miss (First/Take/Last)
		// get a "record not found" line logged on every miss even with
		// showSql off. Rebuild the same Warn-level default (real errors
		// and slow-query warnings still get logged) but with that flag
		// set, so only the routine ErrRecordNotFound case is silenced.
		gormConfig.Logger = logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		})
	}

	var db *gorm.DB
	var err error

	db, err = gorm.Open(postgres.New(gormPgConfig), gormConfig)
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(m.config.MaxIdleConns)
	sqlDB.SetMaxOpenConns(m.config.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Second * time.Duration(m.config.ConnMaxLifetime))

	err = sqlDB.Ping()
	if err != nil {
		return nil, err
	}

	return db, err
}

func (m *Module) autoMigrate() error {

	if !m.config.AutoMigrate {
		return nil
	}

	var err error

	for _, module := range m.manager.GetModulesList() {
		if preMigrationModule, ok := module.(PreMigrationConsumer); ok {
			err = preMigrationModule.DBPreMigration(m.db)
			if err != nil {
				return err
			}
		}
	}

	err = m.autoMigrateTables()
	if err != nil {
		return err
	}

	return nil
}

func (m *Module) autoMigrateTables() error {

	slog.Info("Storage tables auto migration starting ...")

	entities := make([]interface{}, 0)
	for _, module := range m.manager.GetModulesList() {
		if consumer, ok := module.(EntitiesProvider); ok {
			consumerEntities := consumer.GetDBEntities()
			if consumerEntities != nil && len(consumerEntities) > 0 {
				entities = append(entities, consumerEntities...)
			}
		}
	}

	var err error

	err = m.db.AutoMigrate(entities...)

	slog.Info("Storage tables auto migration finished ...")

	return err
}

func (m *Module) notifyConsumers() {
	for _, module := range m.manager.GetModulesList() {
		if consumer, ok := module.(Consumer); ok {
			consumer.SetDB(m.db)
		}
	}
}

func (m *Module) Stop() error {

	if m.db == nil {
		return nil
	}

	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}

	err = sqlDB.Close()
	m.db = nil

	return err
}

func (m *Module) GetDB() *gorm.DB {
	return m.db
}
