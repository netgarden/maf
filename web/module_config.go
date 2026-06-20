package web

type MiddlewareFuncProvider = func() MiddlewareFunc

type ModuleConfig struct {
	Middlewares           []MiddlewareFuncProvider
	StaticPath            string
	StaticNotFoundHandler Handler
}
