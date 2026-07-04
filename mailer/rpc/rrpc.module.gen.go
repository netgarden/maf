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
	return "mailer"
}

func (s *Module) Services() map[string]rrpc.ServiceHandler {
	return s.services
}

func (s *Module) SetMailerService(mailerService MailerService) {
	s.services["Mailer"] = newMailerServiceHandler(mailerService)
}
