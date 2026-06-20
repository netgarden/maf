package web

import (
	"errors"
	"github.com/labstack/echo/v4"
	"io/fs"
)

func NewStaticHandler(fs fs.FS, notFoundHandler Handler) echo.HandlerFunc {

	sh := &StaticHandler{
		defaultHandler: echo.StaticDirectoryHandler(fs, false),
	}

	if notFoundHandler != nil {
		sh.notFoundHandler = Handle(notFoundHandler.Handle)
	}

	return sh.Handle
}

type StaticHandler struct {
	defaultHandler  echo.HandlerFunc
	notFoundHandler echo.HandlerFunc
}

func (sh *StaticHandler) Handle(c echo.Context) error {

	err := sh.defaultHandler(c)
	if sh.notFoundHandler == nil || err == nil || !errors.Is(err, echo.ErrNotFound) {
		return err
	}

	return sh.notFoundHandler(c)

}
