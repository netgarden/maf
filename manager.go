package maf

import (
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/elliotchance/orderedmap"
)

func New(application Application) *Manager {
	return &Manager{
		application: application,
		modules:     orderedmap.NewOrderedMap(),
		running:     false,
	}
}

type Manager struct {
	application Application
	modules     *orderedmap.OrderedMap
	config      *Config
	running     bool
	wg          *sync.WaitGroup
}

func (m *Manager) GetApplication() Application {
	return m.application
}

func (m *Manager) GetModule(name string) Module {

	module, ok := m.modules.Get(name)
	if !ok {
		return nil
	}

	return module.(Module)
}

func (m *Manager) GetModulesList() []Module {
	ret := make([]Module, m.modules.Len())
	i := 0
	for el := m.modules.Front(); el != nil; el = el.Next() {
		ret[i] = el.Value.(Module)
		i++
	}
	return ret
}

func (m *Manager) Start(handleShutdown bool) error {

	if m.running {
		return errors.New("already running")
	}
	m.running = true

	err := m.start()
	if err != nil {
		m.Stop()
		return err
	}

	if handleShutdown {
		go m.handleSignals()

		m.wg = &sync.WaitGroup{}
		m.wg.Add(1)
		m.wg.Wait()

		m.Stop()
	}

	return err
}

func (m *Manager) Stop() {

	if !m.running {
		return
	}

	m.doStop()
	m.running = false
}

func (m *Manager) start() error {

	var err error

	err = m.initModules()
	if err != nil {
		return err
	}

	err = m.initConfig()
	if err != nil {
		return err
	}

	err = m.initLogging()
	if err != nil {
		return err
	}

	err = m.doPreInitialize()
	if err != nil {
		return err
	}

	err = m.doInitialize()
	if err != nil {
		return err
	}

	err = m.doPostInitialize()
	if err != nil {
		return err
	}

	err = m.doPreStart()
	if err != nil {
		return err
	}

	err = m.doStart()
	if err != nil {
		return err
	}

	slog.Info("Application '" + m.application.GetName() + "' is running!")

	return nil
}

func (m *Manager) initModules() error {

	for _, module := range m.application.GetModules() {
		module.SetManager(m)
		m.modules.Set(module.GetID(), module)
	}

	return nil
}

func (m *Manager) initConfig() error {

	schema := make([]ConfigItem, 0)

	for el := m.modules.Front(); el != nil; el = el.Next() {
		if schemaProvider, ok := el.Value.(ModuleConfigSchemaProvider); ok {
			schema = append(schema, schemaProvider.GetConfigSchema()...)
		}
	}

	loader := NewConfigLoader(m)
	config, err := loader.Load(schema)
	if err != nil {
		return err
	}

	m.config = config

	for el := m.modules.Front(); el != nil; el = el.Next() {
		if configConsumer, ok := el.Value.(ModuleConfigConsumer); ok {
			configConsumer.SetConfig(config)
		}
	}

	return nil
}

func (m *Manager) initLogging() error {

	for el := m.modules.Front(); el != nil; el = el.Next() {

		module, ok := el.Value.(ModuleLoggingProvider)
		if !ok {
			continue
		}

		slog.Info("Setting up logging using module: " + module.GetName())
		if err := module.SetupLogging(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doPreInitialize() error {

	for el := m.modules.Front(); el != nil; el = el.Next() {

		module, ok := el.Value.(ModulePreInitialize)
		if !ok {
			continue
		}

		slog.Info("PreInitializing module: " + module.GetName())
		if err := module.PreInitialize(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doInitialize() error {

	for el := m.modules.Front(); el != nil; el = el.Next() {

		module, ok := el.Value.(ModuleInitialize)
		if !ok {
			continue
		}

		slog.Info("Initializing module: " + module.GetName())
		if err := module.Initialize(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doPostInitialize() error {

	for el := m.modules.Front(); el != nil; el = el.Next() {

		module, ok := el.Value.(ModulePostInitialize)
		if !ok {
			continue
		}

		slog.Info("PostInitializing module: " + module.GetName())
		if err := module.PostInitialize(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doPreStart() error {

	for el := m.modules.Front(); el != nil; el = el.Next() {

		module, ok := el.Value.(ModulePreStart)
		if !ok {
			continue
		}

		slog.Info("PreStarting module: " + module.GetName())
		if err := module.PreStart(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doStart() error {

	for el := m.modules.Front(); el != nil; el = el.Next() {

		module, ok := el.Value.(ModuleStart)
		if !ok {
			continue
		}

		slog.Info("Starting module: " + module.GetName())
		if err := module.Start(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doStop() {
	for el := m.modules.Front(); el != nil; el = el.Next() {

		module, ok := el.Value.(ModuleStop)
		if !ok {
			continue
		}

		slog.Info("Stopping module: " + module.GetName())
		if err := module.Stop(); err != nil {
			slog.Error("Error while stopping module "+module.GetName(), slog.Any("error", err))
		}

	}
}

func (m *Manager) handleSignals() {
	func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch,
			os.Interrupt,
			syscall.SIGINT,
			os.Kill,
			syscall.SIGKILL,
			syscall.SIGTERM,
		)
		select {
		case <-ch:
			m.wg.Done()
		}
	}()
}
