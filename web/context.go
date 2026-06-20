package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type HandlerFunc func(c *Context) error

type Handler interface {
	Handle(*Context) error
}

type Context struct {
	echo.Context
	csrfConfig *middleware.CSRFConfig
}

func CustomContext(csrfConfig *middleware.CSRFConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := &Context{
				Context:    c,
				csrfConfig: csrfConfig,
			}
			return next(cc)
		}
	}
}

func (c *Context) View(code int, filename string, obj interface{}) error {
	if mapContext, ok := obj.(map[string]interface{}); ok {
		viewData := mapContext
		viewData["request"] = c.Request()
		viewData["csrf"] = c.CSRF()
		obj = viewData
	}
	return c.Context.Render(code, filename, obj)
}

func (c *Context) DtoError(err error) error {

	switch err.(type) {
	case *NotFoundError:
		return echo.NewHTTPError(http.StatusNotFound, "Not found")
	case *InternalError:
		return echo.NewHTTPError(http.StatusInternalServerError, "Internal server error")
	case *InvalidRequestError:
		return echo.NewHTTPError(http.StatusBadRequest, "Bad request")
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, "Internal server error")
	}

}

func (c *Context) NotFound() error {
	return echo.NewHTTPError(http.StatusNotFound)
}

func (c *Context) InternalServerError(err ...error) error {
	if err != nil && len(err) > 0 {
		slog.Error("", err)
	}
	return echo.NewHTTPError(http.StatusInternalServerError)
}

func Handle(handler HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		return handler(c.(*Context))
	}
}

func (c *Context) CSRF() string {
	return c.Get(middleware.DefaultCSRFConfig.ContextKey).(string)
}

func (c *Context) GetFormData() (map[string]string, error) {

	var err error
	ret := make(map[string]string)

	//c.MultipartForm()
	contentType := c.Request().Header.Get("Content-Type")
	if contentType == "multipart/form-data" {
		err = c.Request().ParseMultipartForm(4 * 1024 * 1024)
	} else if contentType == "application/x-www-form-urlencoded" {
		err = c.Request().ParseForm()
	} else {
		err = errors.New("unsupported content type")
	}

	if err != nil {
		return nil, err
	}
	for k, v := range c.Request().PostForm {
		ret[k] = v[len(v)-1]
	}

	return ret, nil
}

/*
func requestQueryToFilter(c *Context, params map[string]string) (i2bolt.Filter, map[string]string) {

	var subFilters []i2bolt.Filter
	filterParams := make(map[string]string)

	query := c.Request().URL.Query()
	for param, fieldMethod := range params {

		if v, ok := query[param]; ok {
			value := strings.TrimSpace(v[0])
			if value != "" {

				parts := strings.Split(fieldMethod, "__")
				fieldName := parts[0]
				method := ""
				if len(parts) > 1 {
					method = parts[1]
				}

				var valueIntf interface{}
				valueIntf = value

				if method == "is" {
					valueIntf = value == "yes"
				}

				fieldNames := strings.Split(fieldName, "|")
				for _, name := range fieldNames {
					subFilters = append(subFilters, i2bolt.Q(name, method, valueIntf))
					filterParams[param] = value
				}

			} else {
				filterParams[param] = ""
			}

		}

	}

	var filter i2bolt.Filter
	if len(subFilters) > 0 {
		filter = i2bolt.And(subFilters...)
	}

	return filter, filterParams
}
*/
