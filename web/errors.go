package web

import (
	"github.com/labstack/echo/v4"
)

type HTTPError = echo.HTTPError

// Errors
var (
	ErrBadRequest                    = echo.ErrBadRequest                    // HTTP 400 Bad Request
	ErrUnauthorized                  = echo.ErrUnauthorized                  // HTTP 401 Unauthorized
	ErrPaymentRequired               = echo.ErrPaymentRequired               // HTTP 402 Payment Required
	ErrForbidden                     = echo.ErrForbidden                     // HTTP 403 Forbidden
	ErrNotFound                      = echo.ErrNotFound                      // HTTP 404 Not Found
	ErrMethodNotAllowed              = echo.ErrMethodNotAllowed              // HTTP 405 Method Not Allowed
	ErrNotAcceptable                 = echo.ErrNotAcceptable                 // HTTP 406 Not Acceptable
	ErrProxyAuthRequired             = echo.ErrProxyAuthRequired             // HTTP 407 Proxy AuthRequired
	ErrRequestTimeout                = echo.ErrRequestTimeout                // HTTP 408 Request Timeout
	ErrConflict                      = echo.ErrConflict                      // HTTP 409 Conflict
	ErrGone                          = echo.ErrGone                          // HTTP 410 Gone
	ErrLengthRequired                = echo.ErrLengthRequired                // HTTP 411 Length Required
	ErrPreconditionFailed            = echo.ErrPreconditionFailed            // HTTP 412 Precondition Failed
	ErrStatusRequestEntityTooLarge   = echo.ErrStatusRequestEntityTooLarge   // HTTP 413 Payload Too Large
	ErrRequestURITooLong             = echo.ErrRequestURITooLong             // HTTP 414 URI Too Long
	ErrUnsupportedMediaType          = echo.ErrUnsupportedMediaType          // HTTP 415 Unsupported Media Type
	ErrRequestedRangeNotSatisfiable  = echo.ErrRequestedRangeNotSatisfiable  // HTTP 416 Range Not Satisfiable
	ErrExpectationFailed             = echo.ErrExpectationFailed             // HTTP 417 Expectation Failed
	ErrTeapot                        = echo.ErrTeapot                        // HTTP 418 I'm a teapot
	ErrMisdirectedRequest            = echo.ErrMisdirectedRequest            // HTTP 421 Misdirected Request
	ErrUnprocessableEntity           = echo.ErrUnprocessableEntity           // HTTP 422 Unprocessable Entity
	ErrLocked                        = echo.ErrLocked                        // HTTP 423 Locked
	ErrFailedDependency              = echo.ErrFailedDependency              // HTTP 424 Failed Dependency
	ErrTooEarly                      = echo.ErrTooEarly                      // HTTP 425 Too Early
	ErrUpgradeRequired               = echo.ErrUpgradeRequired               // HTTP 426 Upgrade Required
	ErrPreconditionRequired          = echo.ErrPreconditionRequired          // HTTP 428 Precondition Required
	ErrTooManyRequests               = echo.ErrTooManyRequests               // HTTP 429 Too Many Requests
	ErrRequestHeaderFieldsTooLarge   = echo.ErrRequestHeaderFieldsTooLarge   // HTTP 431 Request Header Fields Too Large
	ErrUnavailableForLegalReasons    = echo.ErrUnavailableForLegalReasons    // HTTP 451 Unavailable For Legal Reasons
	ErrInternalServerError           = echo.ErrInternalServerError           // HTTP 500 Internal Server Error
	ErrNotImplemented                = echo.ErrNotImplemented                // HTTP 501 Not Implemented
	ErrBadGateway                    = echo.ErrBadGateway                    // HTTP 502 Bad Gateway
	ErrServiceUnavailable            = echo.ErrServiceUnavailable            // HTTP 503 Service Unavailable
	ErrGatewayTimeout                = echo.ErrGatewayTimeout                // HTTP 504 Gateway Timeout
	ErrHTTPVersionNotSupported       = echo.ErrHTTPVersionNotSupported       // HTTP 505 HTTP Version Not Supported
	ErrVariantAlsoNegotiates         = echo.ErrVariantAlsoNegotiates         // HTTP 506 Variant Also Negotiates
	ErrInsufficientStorage           = echo.ErrInsufficientStorage           // HTTP 507 Insufficient Storage
	ErrLoopDetected                  = echo.ErrLoopDetected                  // HTTP 508 Loop Detected
	ErrNotExtended                   = echo.ErrNotExtended                   // HTTP 510 Not Extended
	ErrNetworkAuthenticationRequired = echo.ErrNetworkAuthenticationRequired // HTTP 511 Network Authentication Required
)
