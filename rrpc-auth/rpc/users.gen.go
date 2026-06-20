package rpc

import (
    "github.com/netgarden/rrpc"
)

// -- Structs ---------------------------------------------

type ListUsersResponse struct {
    Users []UserItem `json:"users"`
}

type UpdateUserRequest struct {
    Username string `json:"username"`
    Email string `json:"email"`
    FirstName string `json:"firstName"`
    LastName string `json:"lastName"`
    Admin bool `json:"admin"`
    Active bool `json:"active"`
}

type UserItem struct {
    Id string `json:"id"`
    Username string `json:"username"`
    Email string `json:"email"`
    FirstName string `json:"firstName"`
    LastName string `json:"lastName"`
    Admin bool `json:"admin"`
    Active bool `json:"active"`
}

// -- Services --------------------------------------------

type UsersService interface {
    List(ctx *rrpc.Context) (*ListUsersResponse, error)
    Update(ctx *rrpc.Context, request *UpdateUserRequest) error
    Delete(ctx *rrpc.Context) error
}

// -- Errors ----------------------------------------------


var (
    ErrUserNotFound  = rrpc.RRPCError{Code: 4, Name: "UserNotFound", Message: "user not found", HTTPStatus: 404}
)


