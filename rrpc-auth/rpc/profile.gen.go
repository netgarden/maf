package rpc

import (
    "github.com/netgarden/rrpc"
)

// -- Structs ---------------------------------------------

type ChangePasswordRequest struct {
    CurrentPassword string `json:"currentPassword"`
    NewPassword string `json:"newPassword"`
}

// -- Services --------------------------------------------

type ProfileService interface {
    ChangePassword(ctx *rrpc.Context, request *ChangePasswordRequest) error
}

// -- Errors ----------------------------------------------


var (
    ErrWrongPassword  = rrpc.RRPCError{Code: 3, Name: "WrongPassword", Message: "wrong current password", HTTPStatus: 400}
)


