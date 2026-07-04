package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/security/passwords"
	"github.com/netgarden/rrpc"
)

func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager *maf.Manager
	config  *maf.Config

	passwordsManager *passwords.Manager
	server           *rrpc.Server
	httpServer       *http.Server
}

func (m *Module) GetID() string {
	return "rrpc-server"
}

func (m *Module) GetName() string {
	return "RRPC Server"
}

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

func (m *Module) GetConfigSchema() []maf.ConfigItem {
	return []maf.ConfigItem{
		{Name: "rrpc.port", Type: maf.Int, DefaultValue: 8000, Required: true},
	}
}

func (m *Module) SetConfig(config *maf.Config) {
	m.config = config
}

func (m *Module) Initialize() error {
	m.server = rrpc.NewServer()
	return nil
}

func (m *Module) PreStart() error {

	for _, module := range m.manager.GetModulesList() {
		if mwProvider, ok := module.(RRPCMiddlewaresProvider); ok {
			m.server.Use(mwProvider.GetRRPCMiddlewares()...)
		}
		if provider, ok := module.(RRPCModulesProvider); ok {
			for _, rrpcModule := range provider.GetRRPCModules() {
				err := m.server.RegisterModule(rrpcModule)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (m *Module) Start() error {

	port := m.config.Sub("rrpc").GetInt("port")
	addr := fmt.Sprintf(":%d", port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	m.httpServer = &http.Server{
		Addr:    addr,
		Handler: m.server,
	}

	go func() {
		if err := m.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	return nil
}

func (m *Module) Stop() error {
	if m.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return m.httpServer.Shutdown(ctx)
}
