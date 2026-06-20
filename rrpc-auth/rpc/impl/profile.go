package impl

import (
	"github.com/netgarden/rrpc"
	mafauth "github.com/netgarden/maf/auth"
	"github.com/netgarden/maf/rrpc-auth/authctx"
	"github.com/netgarden/maf/rrpc-auth/rpc"
)

type profileService interface {
	ChangePassword(userID, currentPassword, newPassword string) (bool, error)
}

func NewProfileService(authService *mafauth.AuthService) rpc.ProfileService {
	return &ProfileServiceImpl{auth: authService}
}

type ProfileServiceImpl struct {
	auth profileService
}

func (s *ProfileServiceImpl) ChangePassword(ctx *rrpc.Context, req *rpc.ChangePasswordRequest) error {
	userID, ok := authctx.GetUserID(ctx)
	if !ok {
		return rpc.ErrUnauthorized
	}

	changed, err := s.auth.ChangePassword(userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if !changed {
		return rpc.ErrWrongPassword
	}

	return nil
}