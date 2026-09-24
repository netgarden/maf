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

// RRPCObserversProvider lets a module see every finished request (route
// template, status, size, duration) without wrapping the handler chain; see
// rrpc.ResponseObserver. Used for metrics.
type RRPCObserversProvider interface {
	GetRRPCObservers() []rrpc.ResponseObserver
}
