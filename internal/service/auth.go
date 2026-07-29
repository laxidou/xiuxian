package service

import (
	"context"
	"net/http"
	"time"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	"google.golang.org/protobuf/types/known/emptypb"

	worldv1 "xiuxian/gen/go/xiuxian/v1"
	"xiuxian/internal/biz"
	"xiuxian/internal/conf"
)

type AuthService struct {
	worldv1.UnimplementedAuthServiceServer
	usecase       *biz.WorldUsecase
	world         *WorldService
	limiter       biz.RateLimiter
	secureCookies bool
}

func NewAuthService(usecase *biz.WorldUsecase, world *WorldService, limiter biz.RateLimiter, config *conf.Server) *AuthService {
	return &AuthService{
		usecase:       usecase,
		world:         world,
		limiter:       limiter,
		secureCookies: config.SecureCookie,
	}
}

func (service *AuthService) Register(ctx context.Context, request *worldv1.RegisterRequest) (*worldv1.AuthResponse, error) {
	if err := service.limitPublicAuth(ctx, "registration", "", biz.RegistrationRateLimit); err != nil {
		return nil, err
	}
	token, state, err := service.usecase.Register(ctx, request.Account, request.Password, request.RoleName)
	if err != nil {
		return nil, mapError(err)
	}
	service.setSessionCookie(ctx, token, int((24*time.Hour).Seconds()))
	return &worldv1.AuthResponse{State: RoleState(state)}, nil
}

func (service *AuthService) Login(ctx context.Context, request *worldv1.LoginRequest) (*worldv1.AuthResponse, error) {
	if err := service.limitPublicAuth(ctx, "login", request.Account, biz.LoginRateLimit); err != nil {
		return nil, err
	}
	token, state, err := service.usecase.Login(ctx, request.Account, request.Password)
	if err != nil {
		return nil, mapError(err)
	}
	service.setSessionCookie(ctx, token, int((24*time.Hour).Seconds()))
	return &worldv1.AuthResponse{State: RoleState(state)}, nil
}

func (service *AuthService) Logout(ctx context.Context, _ *worldv1.LogoutRequest) (*emptypb.Empty, error) {
	if request, ok := kratoshttp.RequestFromServerContext(ctx); ok {
		if cookie, err := request.Cookie("xiuxian_session"); err == nil {
			if err := service.usecase.Logout(ctx, cookie.Value); err != nil {
				return nil, mapError(err)
			}
		}
	}
	service.setSessionCookie(ctx, "", -1)
	return &emptypb.Empty{}, nil
}

func (service *AuthService) RotateMCPKey(ctx context.Context, _ *worldv1.RotateMCPKeyRequest) (*worldv1.RotateMCPKeyResponse, error) {
	roleID, err := service.world.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	key, err := service.usecase.RotateMCPKey(ctx, roleID)
	if err != nil {
		return nil, mapError(err)
	}
	return &worldv1.RotateMCPKeyResponse{ApiKey: key}, nil
}

func (service *AuthService) RevokeMCPKey(ctx context.Context, _ *worldv1.RevokeMCPKeyRequest) (*emptypb.Empty, error) {
	roleID, err := service.world.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if err := service.usecase.RevokeMCPKey(ctx, roleID); err != nil {
		return nil, mapError(err)
	}
	return &emptypb.Empty{}, nil
}

func (service *AuthService) limitPublicAuth(ctx context.Context, scope, account string, policy biz.RateLimitPolicy) error {
	subject := account
	if request, ok := kratoshttp.RequestFromServerContext(ctx); ok {
		subject = clientAddress(request) + "\x00" + account
	}
	allowed, err := service.limiter.Allow(ctx, scope, subject, policy)
	if err != nil {
		return ErrorRateLimitUnavailable("rate limiter unavailable")
	}
	if !allowed {
		return ErrorRateLimited("rate limit exceeded")
	}
	return nil
}

func (service *AuthService) setSessionCookie(ctx context.Context, token string, maxAge int) {
	writer, ok := kratoshttp.ResponseWriterFromServerContext(ctx)
	if !ok {
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     "xiuxian_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   service.secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	})
}
