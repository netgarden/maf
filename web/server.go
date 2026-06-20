package web

import (
	"context"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"log/slog"
	"net"
	"net/http"
	"syscall"
	"time"
)

type Server struct {
	module     *Module
	listener   *net.Listener
	csrfConfig *middleware.CSRFConfig
	router     *echo.Echo
	server     *http.Server
}

func NewServer(module *Module) *Server {

	s := &Server{
		module: module,
	}

	s.init()

	return s
}

func (s *Server) init() {

	//s.csrfConfig = &middleware.CSRFConfig{
	//	TokenLookup: "form:_csrf,query:csrf,header:X-CSRF-Token",
	//}

	s.router = echo.New()
	//s.router.Pre(middleware.RemoveTrailingSlash())
	s.router.Use(middleware.Logger())
	s.router.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		LogLevel: 4,
	}))
	//s.router.Use(middleware.CSRFWithConfig(*s.csrfConfig))
	s.router.Use(CustomContext(s.csrfConfig))
	if s.module.moduleConfig != nil && s.module.moduleConfig.Middlewares != nil {
		for _, m := range s.module.moduleConfig.Middlewares {
			s.router.Use(m())
		}
	}

	tplFS := s.module.getTemplatesFS()
	if tplFS != nil {
		s.router.Renderer = NewRenderer(tplFS)
	}
}

func (s *Server) PreStart() {

	staticFs := s.module.getStaticFS()
	if staticFs != nil && s.module.moduleConfig.StaticNotFoundHandler != nil {
		path := s.module.moduleConfig.StaticPath
		if path == "" {
			path = "/static/"
		}
		s.router.Add("GET", path+"*", NewStaticHandler(staticFs, s.module.moduleConfig.StaticNotFoundHandler))
	}

}

func (s *Server) Start() error {

	config := &net.ListenConfig{Control: s.reusePort}
	listener, err := config.Listen(
		context.Background(),
		"tcp",
		fmt.Sprintf(":%d", s.module.config.Port),
	)
	if err != nil {
		return err
	}

	s.listener = &listener

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.module.config.Port),
		Handler: s.router,
	}

	go s.server.Serve(listener)

	return nil
}

func (s *Server) Stop() error {

	if s.server == nil {
		return nil
	}

	timeout := 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.server.Shutdown(ctx)
}

func (s *Server) GetGroup() *Group {
	return &Group{
		s.router.Group(""),
	}
}

func (s *Server) GetRenderer() *Renderer {
	return s.router.Renderer.(*Renderer)
}

func (s *Server) reusePort(network, address string, conn syscall.RawConn) error {
	return conn.Control(func(descriptor uintptr) {
		err := syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		if err != nil {
			slog.Error("error during setting reuseport", err)
		}
	})
}
