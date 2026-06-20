package web

import (
	"github.com/labstack/echo/v4"
	"strings"
)

type echoGroup = echo.Group
type Group struct {
	*echoGroup
}

func (g *Group) Group(prefix string, middleware ...MiddlewareFunc) *Group {
	return &Group{
		g.echoGroup.Group(prefix, middleware...),
	}
}

func (g *Group) AddMany(methods string, path string, handler echo.HandlerFunc) {

	for _, method := range strings.Split(methods, " ") {
		method = strings.TrimSpace(method)
		if method != "" {
			g.Add(method, path, handler)
		}
	}

}
