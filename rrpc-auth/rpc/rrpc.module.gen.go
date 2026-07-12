package rpc

import (
	"github.com/netgarden/rrpc"
)

func NewModule() *Module {
	return &Module{
		services: make(map[string]rrpc.ServiceHandler),
	}
}

type Module struct {
	services map[string]rrpc.ServiceHandler
}

func (s *Module) Name() string {
	return "auth"
}

func (s *Module) Services() map[string]rrpc.ServiceHandler {
	return s.services
}

func (s *Module) SetAuthService(authService AuthService) {
	s.services["Auth"] = newAuthServiceHandler(authService)
}

func (s *Module) SetOIDCAuthService(oIDCAuthService OIDCAuthService) {
	s.services["OIDCAuth"] = newOIDCAuthServiceHandler(oIDCAuthService)
}

func (s *Module) SetOIDCProvidersService(oIDCProvidersService OIDCProvidersService) {
	s.services["OIDCProviders"] = newOIDCProvidersServiceHandler(oIDCProvidersService)
}

func (s *Module) SetProfileService(profileService ProfileService) {
	s.services["Profile"] = newProfileServiceHandler(profileService)
}

func (s *Module) SetProvidersService(providersService ProvidersService) {
	s.services["Providers"] = newProvidersServiceHandler(providersService)
}

func (s *Module) SetPublicProvidersService(publicProvidersService PublicProvidersService) {
	s.services["PublicProviders"] = newPublicProvidersServiceHandler(publicProvidersService)
}

func (s *Module) SetUsersService(usersService UsersService) {
	s.services["Users"] = newUsersServiceHandler(usersService)
}
