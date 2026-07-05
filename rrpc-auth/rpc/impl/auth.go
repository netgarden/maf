package impl

import (
	"net/http"

	mafauth "github.com/netgarden/maf/auth"
	authdto "github.com/netgarden/maf/auth/dto"
	authentities "github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/rrpc-auth/authctx"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
)

type authService interface {
	Login(req *authdto.CredentialsLoginRequest, clientIP, userAgent string, secure bool) (string, *http.Cookie, error)
	Logout(sessionID string, secure bool) *http.Cookie
	Refresh(sessionID string) (string, error)
	GetUser(id string) (*authentities.User, error)
	SessionCookieName() string
	RequestPasswordReset(username string) error
	ConfirmPasswordReset(token, newPassword string) (bool, error)
}

func NewAuthService(authService *mafauth.AuthService) rpc.AuthService {
	return &AuthServiceImpl{auth: authService}
}

type AuthServiceImpl struct {
	auth authService
}

func (s *AuthServiceImpl) Login(ctx *rrpc.Context, req *rpc.LoginRequest) (*rpc.LoginResponse, error) {
	r := ctx.Request()
	accessToken, refreshCookie, err := s.auth.Login(
		&authdto.CredentialsLoginRequest{Username: req.Username, Password: req.Password},
		r.RemoteAddr,
		r.Header.Get("User-Agent"),
		ctx.IsHTTPS(),
	)
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if accessToken == "" {
		return nil, rpc.ErrBadCredentials
	}
	http.SetCookie(ctx.Response(), refreshCookie)
	return &rpc.LoginResponse{AccessToken: accessToken}, nil
}

func (s *AuthServiceImpl) Logout(ctx *rrpc.Context) error {
	var sessionID string
	if cookie, err := ctx.Request().Cookie(s.auth.SessionCookieName()); err == nil {
		sessionID = cookie.Value
	}
	http.SetCookie(ctx.Response(), s.auth.Logout(sessionID, ctx.IsHTTPS()))
	return nil
}

func (s *AuthServiceImpl) Refresh(ctx *rrpc.Context) (*rpc.RefreshResponse, error) {
	cookie, err := ctx.Request().Cookie(s.auth.SessionCookieName())
	if err != nil {
		return nil, rpc.ErrUnauthorized
	}
	newAccessToken, err := s.auth.Refresh(cookie.Value)
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if newAccessToken == "" {
		return nil, rpc.ErrUnauthorized
	}
	return &rpc.RefreshResponse{AccessToken: newAccessToken}, nil
}

func (s *AuthServiceImpl) Me(ctx *rrpc.Context) (*rpc.MeResponse, error) {
	userID, ok := authctx.GetUserID(ctx)
	if !ok {
		return nil, rpc.ErrUnauthorized
	}
	user, err := s.auth.GetUser(userID)
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if user == nil {
		return nil, rpc.ErrUnauthorized
	}
	return &rpc.MeResponse{
		Id:        user.ID.String(),
		Username:  user.Username,
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Admin:     user.Admin,
	}, nil
}

func (s *AuthServiceImpl) RequestPasswordReset(ctx *rrpc.Context, req *rpc.RequestPasswordResetRequest) error {
	if err := s.auth.RequestPasswordReset(req.Username); err != nil {
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	return nil
}

func (s *AuthServiceImpl) ConfirmPasswordReset(ctx *rrpc.Context, req *rpc.ConfirmPasswordResetRequest) error {
	ok, err := s.auth.ConfirmPasswordReset(req.Token, req.NewPassword)
	if err != nil {
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if !ok {
		return rpc.ErrInvalidResetToken
	}
	return nil
}
