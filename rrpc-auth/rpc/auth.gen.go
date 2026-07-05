package rpc

import (
	"github.com/netgarden/rrpc"
)

// -- Structs ---------------------------------------------

type ConfirmPasswordResetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	AccessToken string `json:"accessToken"`
}

type MeResponse struct {
	Id        string `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Admin     bool   `json:"admin"`
}

type RefreshResponse struct {
	AccessToken string `json:"accessToken"`
}

type RequestPasswordResetRequest struct {
	Username string `json:"username"`
}

// -- Services --------------------------------------------

type AuthService interface {
	Login(ctx *rrpc.Context, request *LoginRequest) (*LoginResponse, error)
	Logout(ctx *rrpc.Context) error
	Refresh(ctx *rrpc.Context) (*RefreshResponse, error)
	Me(ctx *rrpc.Context) (*MeResponse, error)
	RequestPasswordReset(ctx *rrpc.Context, request *RequestPasswordResetRequest) error
	ConfirmPasswordReset(ctx *rrpc.Context, request *ConfirmPasswordResetRequest) error
}

// -- Errors ----------------------------------------------

var (
	ErrBadCredentials = rrpc.RRPCError{Code: 2, Name: "BadCredentials", Message: "invalid credentials", HTTPStatus: 401}

	ErrInvalidResetToken = rrpc.RRPCError{Code: 6, Name: "InvalidResetToken", Message: "invalid or expired reset token", HTTPStatus: 400}

	ErrUnauthorized = rrpc.RRPCError{Code: 1, Name: "Unauthorized", Message: "unauthorized", HTTPStatus: 401}
)
