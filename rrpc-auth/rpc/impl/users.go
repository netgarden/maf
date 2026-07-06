package impl

import (
	"errors"

	mafauthdto "github.com/netgarden/maf/auth/dto"
	mafauth "github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/datatables"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
)

func NewUsersService(svc *mafauth.UsersService) rpc.UsersService {
	return &UsersServiceImpl{users: svc}
}

type UsersServiceImpl struct {
	users *mafauth.UsersService
}

func (s *UsersServiceImpl) List(ctx *rrpc.Context, req *rpc.DatatableRequest) (*rpc.ListUsersResponse, error) {
	result, err := s.users.ListUsersPage(datatables.Query{
		Page:     req.Page,
		PageSize: req.PageSize,
		SortBy:   req.SortBy,
		SortDir:  datatables.SortDir(req.SortDir),
		Filters:  req.Filters,
	})
	if err != nil {
		if errors.Is(err, datatables.ErrUnknownSortColumn) || errors.Is(err, datatables.ErrUnknownFilterColumn) {
			return nil, rrpc.ErrRrpcBadRequest.WithCause(err)
		}
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}

	items := make([]rpc.UserItem, 0, len(result.Items))
	for _, u := range result.Items {
		items = append(items, rpc.UserItem{
			Id:        u.ID.String(),
			Username:  u.Username,
			Email:     u.Email,
			FirstName: u.FirstName,
			LastName:  u.LastName,
			Admin:     u.Admin,
			Active:    u.Active,
		})
	}

	return &rpc.ListUsersResponse{
		Users: items,
		PageInfo: rpc.DatatablePageInfo{
			TotalCount: int(result.TotalCount),
			Page:       req.Page,
			PageSize:   req.PageSize,
		},
	}, nil
}

func (s *UsersServiceImpl) Create(ctx *rrpc.Context, req *rpc.CreateUserRequest) (*rpc.UserItem, error) {
	created, err := s.users.CreateUser(&mafauthdto.UserCreateDTO{
		Username:             req.Username,
		Password:             req.Password,
		Email:                req.Email,
		FirstName:            req.FirstName,
		LastName:             req.LastName,
		Admin:                req.Admin,
		SendCredentialsEmail: req.SendCredentialsEmail,
	})
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if created == nil {
		return nil, rpc.ErrUserAlreadyExists
	}
	return &rpc.UserItem{
		Id:        created.ID.String(),
		Username:  created.Username,
		Email:     created.Email,
		FirstName: created.FirstName,
		LastName:  created.LastName,
		Admin:     created.Admin,
		Active:    created.Active,
	}, nil
}

func (s *UsersServiceImpl) Update(ctx *rrpc.Context, req *rpc.UpdateUserRequest) error {
	id := ctx.Params().GetString("id")
	updated, err := s.users.UpdateUser(id, &mafauthdto.UserUpdateDTO{
		Username:  req.Username,
		Email:     req.Email,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Admin:     req.Admin,
		Active:    req.Active,
	})
	if err != nil {
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if updated == nil {
		return rpc.ErrUserNotFound
	}
	return nil
}

func (s *UsersServiceImpl) Delete(ctx *rrpc.Context) error {
	id := ctx.Params().GetString("id")
	found, err := s.users.DeleteUser(id)
	if err != nil {
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if !found {
		return rpc.ErrUserNotFound
	}
	return nil
}
