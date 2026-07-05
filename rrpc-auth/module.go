package rrpcauth

import (
	"errors"

	"github.com/netgarden/maf"
	mafauth "github.com/netgarden/maf/auth"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/maf/rrpc-auth/rpc/impl"
	"github.com/netgarden/rrpc"
)

func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager    *maf.Manager
	authModule *mafauth.Module
	rrpcModule *rpc.Module
}

func (m *Module) GetID() string   { return "rrpc-auth" }
func (m *Module) GetName() string { return "RRPC Auth" }

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

func (m *Module) GetDependencies() []string {
	return []string{"auth"}
}

// Initialize is pure plumbing — rrpc-auth is only the RPC interface for
// maf/auth's services, so it does no business-logic wiring of its own
// (e.g. registering mailer templates: see auth.Module.Initialize instead).
func (m *Module) Initialize() error {
	authMod, ok := m.manager.GetModule("auth").(*mafauth.Module)
	if !ok {
		return errors.New("auth module not found or wrong type — register auth before rrpc-auth")
	}
	m.authModule = authMod

	authSvc := authMod.GetServicesManager().GetAuthService()
	usersSvc := authMod.GetServicesManager().GetUsersService()

	m.rrpcModule = rpc.NewModule()
	m.rrpcModule.SetAuthService(impl.NewAuthService(authSvc))
	m.rrpcModule.SetProfileService(impl.NewProfileService(authSvc))
	m.rrpcModule.SetUsersService(impl.NewUsersService(usersSvc))

	return nil
}

func (m *Module) GetRRPCModules() []rrpc.ServerModule {
	return []rrpc.ServerModule{
		m.rrpcModule,
	}
}

// AuthenticationMiddleware returns a middleware that validates Bearer tokens
// and stores the user ID in context. Wire it before AuthorizationMiddleware.
func (m *Module) AuthenticationMiddleware() rrpc.Middleware {
	return NewAuthenticationMiddleware(m.authModule.GetServicesManager().GetAuthService())
}
