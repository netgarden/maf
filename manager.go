package maf

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
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
	// startOrder is modules ordered so that every module's declared
	// dependencies (ModuleDependenciesProvider) come before it. Computed by
	// resolveStartOrder and used to drive every lifecycle phase. Modules
	// with no ordering constraint between them keep their relative
	// registration order.
	startOrder []Module
	config     *Config
	running    bool
	wg         *sync.WaitGroup
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

// GetModulesList returns all registered modules in registration order (the
// order returned by Application.GetModules). For dependency-respecting
// lifecycle order, see resolveStartOrder / startOrder.
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

	err = m.checkDependencies()
	if err != nil {
		return err
	}

	err = m.resolveStartOrder()
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

// checkDependencies fails startup immediately if any registered module
// implementing ModuleDependenciesProvider declares a dependency on a
// module ID that isn't also registered.
func (m *Manager) checkDependencies() error {

	for el := m.modules.Front(); el != nil; el = el.Next() {

		module, ok := el.Value.(ModuleDependenciesProvider)
		if !ok {
			continue
		}

		for _, dependencyID := range module.GetDependencies() {
			if _, found := m.modules.Get(dependencyID); !found {
				return fmt.Errorf(
					"module %q depends on module %q, which is not registered",
					module.GetID(),
					dependencyID,
				)
			}
		}
	}

	return nil
}

// resolveStartOrder topologically sorts registered modules so every
// module's declared dependencies come before it (Kahn's algorithm), and
// stores the result in m.startOrder for every lifecycle phase to use.
// Modules with no ordering constraint between them keep their relative
// registration order, so applications that don't use
// ModuleDependenciesProvider at all see no change in behavior.
//
// Must run after checkDependencies, which guarantees every declared
// dependency ID refers to a registered module — this only has to handle
// ordering and cycles.
func (m *Manager) resolveStartOrder() error {

	modules := m.GetModulesList()

	indexOf := make(map[string]int, len(modules))
	for i, module := range modules {
		indexOf[module.GetID()] = i
	}

	// inDegree[i] = number of modules[i]'s dependencies not yet placed.
	inDegree := make([]int, len(modules))
	// dependents[i] = indices of modules that declare modules[i] as a dependency.
	dependents := make([][]int, len(modules))

	for i, module := range modules {
		dependenciesProvider, ok := module.(ModuleDependenciesProvider)
		if !ok {
			continue
		}
		for _, dependencyID := range dependenciesProvider.GetDependencies() {
			dependencyIndex := indexOf[dependencyID]
			dependents[dependencyIndex] = append(dependents[dependencyIndex], i)
			inDegree[i]++
		}
	}

	placed := make([]bool, len(modules))
	order := make([]Module, 0, len(modules))

	for len(order) < len(modules) {

		// Pick the earliest-registered not-yet-placed module with no
		// remaining unplaced dependencies, so the result is deterministic
		// and matches registration order wherever dependencies allow it.
		next := -1
		for i := range modules {
			if !placed[i] && inDegree[i] == 0 {
				next = i
				break
			}
		}

		if next == -1 {
			cycle := findDependencyCycle(modules, indexOf, placed)
			return fmt.Errorf("circular module dependency detected: %s", strings.Join(cycle, " -> "))
		}

		placed[next] = true
		order = append(order, modules[next])

		for _, dependentIndex := range dependents[next] {
			inDegree[dependentIndex]--
		}
	}

	m.startOrder = order

	return nil
}

// findDependencyCycle finds one cycle among the modules not yet placed by
// resolveStartOrder's Kahn's-algorithm pass, via DFS over the "depends on"
// edges. Only called once that pass has stalled with unplaced modules
// remaining, which guarantees a cycle exists among them.
func findDependencyCycle(modules []Module, indexOf map[string]int, placed []bool) []string {

	const (
		white = iota // not yet visited
		gray         // on the current DFS path
		black        // fully explored, no cycle found through it
	)

	color := make([]int, len(modules))
	for i, done := range placed {
		if done {
			color[i] = black
		}
	}

	var path []int
	var cycle []int

	var visit func(i int) bool
	visit = func(i int) bool {

		color[i] = gray
		path = append(path, i)

		if dependenciesProvider, ok := modules[i].(ModuleDependenciesProvider); ok {
			for _, dependencyID := range dependenciesProvider.GetDependencies() {

				dependencyIndex := indexOf[dependencyID]

				switch color[dependencyIndex] {
				case white:
					if visit(dependencyIndex) {
						return true
					}
				case gray:
					start := 0
					for p, node := range path {
						if node == dependencyIndex {
							start = p
							break
						}
					}
					cycle = append(append([]int{}, path[start:]...), dependencyIndex)
					return true
				}
			}
		}

		path = path[:len(path)-1]
		color[i] = black

		return false
	}

	for i := range modules {
		if color[i] == white {
			if visit(i) {
				break
			}
		}
	}

	ids := make([]string, len(cycle))
	for i, index := range cycle {
		ids[i] = modules[index].GetID()
	}

	return ids
}

func (m *Manager) initConfig() error {

	schema := make([]ConfigItem, 0)

	for _, module := range m.startOrder {
		if schemaProvider, ok := module.(ModuleConfigSchemaProvider); ok {
			schema = append(schema, schemaProvider.GetConfigSchema()...)
		}
	}

	loader := NewConfigLoader(m)
	config, err := loader.Load(schema)
	if err != nil {
		return err
	}

	m.config = config

	for _, module := range m.startOrder {
		if configConsumer, ok := module.(ModuleConfigConsumer); ok {
			configConsumer.SetConfig(config)
		}
	}

	return nil
}

func (m *Manager) initLogging() error {

	for _, module := range m.startOrder {

		provider, ok := module.(ModuleLoggingProvider)
		if !ok {
			continue
		}

		slog.Info("Setting up logging using module: " + provider.GetName())
		if err := provider.SetupLogging(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doPreInitialize() error {

	for _, module := range m.startOrder {

		provider, ok := module.(ModulePreInitialize)
		if !ok {
			continue
		}

		slog.Info("PreInitializing module: " + provider.GetName())
		if err := provider.PreInitialize(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doInitialize() error {

	for _, module := range m.startOrder {

		provider, ok := module.(ModuleInitialize)
		if !ok {
			continue
		}

		slog.Info("Initializing module: " + provider.GetName())
		if err := provider.Initialize(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doPostInitialize() error {

	for _, module := range m.startOrder {

		provider, ok := module.(ModulePostInitialize)
		if !ok {
			continue
		}

		slog.Info("PostInitializing module: " + provider.GetName())
		if err := provider.PostInitialize(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doPreStart() error {

	for _, module := range m.startOrder {

		provider, ok := module.(ModulePreStart)
		if !ok {
			continue
		}

		slog.Info("PreStarting module: " + provider.GetName())
		if err := provider.PreStart(); err != nil {
			return err
		}

	}

	return nil
}

func (m *Manager) doStart() error {

	for _, module := range m.startOrder {

		provider, ok := module.(ModuleStart)
		if !ok {
			continue
		}

		slog.Info("Starting module: " + provider.GetName())
		if err := provider.Start(); err != nil {
			return err
		}

	}

	return nil
}

// doStop stops modules in the reverse of startOrder, so a module is always
// stopped before the dependencies it declared (which may still be in use
// during its own Stop).
func (m *Manager) doStop() {
	for i := len(m.startOrder) - 1; i >= 0; i-- {

		provider, ok := m.startOrder[i].(ModuleStop)
		if !ok {
			continue
		}

		slog.Info("Stopping module: " + provider.GetName())
		if err := provider.Stop(); err != nil {
			slog.Error("Error while stopping module "+provider.GetName(), slog.Any("error", err))
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
