package rpc

import (
	"github.com/netgarden/rrpc"
)

// -- Structs ---------------------------------------------

type CreateUserRequest struct {
	Username             string `json:"username"`
	Password             string `json:"password"`
	Email                string `json:"email"`
	FirstName            string `json:"firstName"`
	LastName             string `json:"lastName"`
	Admin                bool   `json:"admin"`
	SendCredentialsEmail bool   `json:"sendCredentialsEmail"`
}

type ListUsersResponse struct {
	Users    []UserItem        `json:"users"`
	PageInfo DatatablePageInfo `json:"pageInfo"`
}

type UpdateUserRequest struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Admin     bool   `json:"admin"`
	Active    bool   `json:"active"`
}

type UserItem struct {
	Id        string `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Admin     bool   `json:"admin"`
	Active    bool   `json:"active"`
}

// -- Services --------------------------------------------

type UsersService interface {
	List(ctx *rrpc.Context, request *DatatableRequest) (*ListUsersResponse, error)
	Create(ctx *rrpc.Context, request *CreateUserRequest) (*UserItem, error)
	Update(ctx *rrpc.Context, request *UpdateUserRequest) error
	Delete(ctx *rrpc.Context) error
}

// -- Errors ----------------------------------------------

var (
	ErrUserAlreadyExists = rrpc.RRPCError{Code: 5, Name: "UserAlreadyExists", Message: "username already taken", HTTPStatus: 409}

	ErrUserNotFound = rrpc.RRPCError{Code: 4, Name: "UserNotFound", Message: "user not found", HTTPStatus: 404}
)
