package rrpcauth

import (
	"errors"
	"net/http"

	"github.com/netgarden/maf"
	mafauth "github.com/netgarden/maf/auth"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/maf/rrpc-auth/rpc/impl"
	"github.com/netgarden/rrpc"
)

// Option configures a Module at construction time, before Initialize runs.
type Option func(*Module)

// WithClientIP wires a trusted-proxy-aware resolver (e.g. an
// *ipfilter.Resolver's ClientIP method value — maf/security/ipfilter,
// already used by iris's own webhook/metrics wiring) so an API key's
// optional AllowedIPs restriction is checked against the caller's real
// address, not a fronting reverse proxy's. Omitting this option is safe: the
// authentication middleware then falls back to the request's own
// RemoteAddr, the same as an unconfigured ipfilter.Resolver would.
func WithClientIP(resolve func(*http.Request) string) Option {
	return func(m *Module) { m.clientIP = resolve }
}

func NewModule(opts ...Option) *Module {
	m := &Module{}
	for _, o := range opts {
		o(m)
	}
	return m
}

type Module struct {
	manager    *maf.Manager
	authModule *mafauth.Module
	rrpcModule *rpc.Module
	clientIP   func(*http.Request) string
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
	providersSvc := authMod.GetServicesManager().GetProvidersService()
	oidcAuthSvc := authMod.GetServicesManager().GetOIDCAuthService()
	oidcProvidersSvc := authMod.GetServicesManager().GetOIDCProvidersService()
	apiKeysSvc := authMod.GetServicesManager().GetApiKeysService()

	m.rrpcModule = rpc.NewModule()
	m.rrpcModule.SetAuthService(impl.NewAuthService(authSvc))
	m.rrpcModule.SetProfileService(impl.NewProfileService(authSvc))
	m.rrpcModule.SetUsersService(impl.NewUsersService(usersSvc))
	m.rrpcModule.SetProvidersService(impl.NewProvidersService(providersSvc))
	m.rrpcModule.SetPublicProvidersService(impl.NewPublicProvidersService(providersSvc))
	m.rrpcModule.SetOIDCAuthService(impl.NewOIDCAuthService(oidcAuthSvc))
	m.rrpcModule.SetOIDCProvidersService(impl.NewOIDCProvidersService(oidcProvidersSvc))
	m.rrpcModule.SetApiKeysService(impl.NewApiKeysService(apiKeysSvc))

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
	return NewAuthenticationMiddleware(m.authModule.GetServicesManager().GetAuthService(), m.clientIP)
}
