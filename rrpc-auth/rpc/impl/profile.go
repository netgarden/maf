package impl

import (
	mafauth "github.com/netgarden/maf/auth"
	"github.com/netgarden/maf/rrpc-auth/authctx"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
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
	// ChangePassword only ever changes the caller's own password (there is no
	// "other user" form of it) — an API-key-authenticated caller is refused
	// unconditionally, so a leaked or agent-held key can't lock the real
	// owner out by rotating their password out from under them.
	if authctx.IsApiKeyAuth(ctx) {
		return rpc.ErrApiKeyRestricted
	}

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
