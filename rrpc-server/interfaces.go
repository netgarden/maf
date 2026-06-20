package security

import (
	"github.com/netgarden/rrpc"
)

type RRPCModulesProvider interface {
	GetRRPCModules() []rrpc.ServerModule
}

type RRPCMiddlewaresProvider interface {
	GetRRPCMiddlewares() []rrpc.Middleware
}
