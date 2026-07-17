package biz

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"

	"xiuxian/internal/rules"
	"xiuxian/internal/world"
)

type (
	State               = world.State
	Bounds              = world.Bounds
	ScanResult          = world.ScanResult
	Event               = world.Event
	Conversation        = world.Conversation
	ConversationMessage = world.ConversationMessage
	CommandExpectation  = world.CommandExpectation
	PublicPosition      = world.PublicPosition
)

var (
	ErrConflict        = world.ErrConflict
	ErrInvalid         = world.ErrInvalid
	ErrUnauthenticated = world.ErrUnauthenticated
	ErrNotAlive        = world.ErrNotAlive
	ErrIdempotencyKey  = world.ErrIdempotencyKey
	ErrNotFound        = world.ErrNotFound
	ErrForbidden       = world.ErrForbidden
	ErrRateLimited     = world.ErrRateLimited
	ErrStaleCommand    = world.ErrStaleCommand
)

// WorldRepository is the persistence contract required by the world authority.
// It is defined in the biz layer so infrastructure remains replaceable.
type WorldRepository interface {
	Load(context.Context) ([]byte, error)
	Save(context.Context, []byte) error
}

// WorldAuthority captures the domain operations consumed by application use
// cases. The concrete implementation remains isolated in internal/world.
type WorldAuthority interface {
	Clock() world.Clock
	Register(string, string, string) (string, world.State, error)
	Login(string, string) (string, world.State, error)
	AuthenticateSession(string) (string, error)
	Logout(string) error
	AuthenticateAPIKey(string) (string, error)
	RotateMCPKey(string) (string, error)
	RevokeMCPKey(string) error
	State(string) (world.State, error)
	SettleDeadline(string, int64) (bool, error)
	Move(string, string, rules.Position, world.CommandExpectation) (world.State, error)
	Stop(string, string, world.CommandExpectation) (world.State, error)
	Scan(string, world.CommandExpectation) (world.ScanResult, error)
	Transfer(string, string, string, int64, world.CommandExpectation) (world.State, error)
	Seize(string, string, string, world.CommandExpectation) (world.State, error)
	RequestConversation(string, string, string, world.CommandExpectation) (world.Conversation, error)
	RespondConversation(string, string, string, string, world.CommandExpectation) (world.Conversation, error)
	SendConversationMessage(string, string, string, string, world.CommandExpectation) (world.ConversationMessage, error)
	CloseConversation(string, string, string, world.CommandExpectation) (world.Conversation, error)
	Conversations(string) ([]world.Conversation, error)
	Events(string, int64, int) ([]world.Event, error)
	Bounds() world.Bounds
	Reincarnate(string, string, *rules.Position, world.CommandExpectation) (world.State, error)
}

func NewWorldAuthority(repository WorldRepository) (*world.Service, error) {
	return world.NewPersistentService(context.Background(), world.SystemClock{}, repository)
}

// WorldUsecase is the application boundary shared by HTTP and gRPC transports.
type WorldUsecase struct {
	authority WorldAuthority
	log       *log.Helper
}

func NewWorldUsecase(authority WorldAuthority, logger log.Logger) *WorldUsecase {
	return &WorldUsecase{authority: authority, log: log.NewHelper(logger)}
}

func (uc *WorldUsecase) Clock() world.Clock { return uc.authority.Clock() }

func (uc *WorldUsecase) Register(ctx context.Context, account, password, roleName string) (string, State, error) {
	if err := ctx.Err(); err != nil {
		return "", State{}, err
	}
	token, state, err := uc.authority.Register(account, password, roleName)
	uc.logFailure(ctx, "register", err)
	return token, state, err
}

func (uc *WorldUsecase) Login(ctx context.Context, account, password string) (string, State, error) {
	if err := ctx.Err(); err != nil {
		return "", State{}, err
	}
	token, state, err := uc.authority.Login(account, password)
	uc.logFailure(ctx, "login", err)
	return token, state, err
}

func (uc *WorldUsecase) AuthenticateSession(ctx context.Context, token string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return uc.authority.AuthenticateSession(token)
}

func (uc *WorldUsecase) Logout(ctx context.Context, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return uc.authority.Logout(token)
}

func (uc *WorldUsecase) AuthenticateAPIKey(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return uc.authority.AuthenticateAPIKey(key)
}

func (uc *WorldUsecase) RotateMCPKey(ctx context.Context, roleID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return uc.authority.RotateMCPKey(roleID)
}

func (uc *WorldUsecase) RevokeMCPKey(ctx context.Context, roleID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return uc.authority.RevokeMCPKey(roleID)
}

func (uc *WorldUsecase) State(ctx context.Context, roleID string) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.State(roleID)
}

func (uc *WorldUsecase) SettleDeadline(ctx context.Context, roleID string, expectedVersion int64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return uc.authority.SettleDeadline(roleID, expectedVersion)
}

func (uc *WorldUsecase) Move(ctx context.Context, roleID, idempotencyKey string, target rules.Position, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.Move(roleID, idempotencyKey, target, expectation)
}

func (uc *WorldUsecase) Stop(ctx context.Context, roleID, idempotencyKey string, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.Stop(roleID, idempotencyKey, expectation)
}

func (uc *WorldUsecase) Scan(ctx context.Context, roleID string, expectation CommandExpectation) (ScanResult, error) {
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}
	return uc.authority.Scan(roleID, expectation)
}

func (uc *WorldUsecase) Transfer(ctx context.Context, roleID, targetID, idempotencyKey string, amountMinutes int64, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.Transfer(roleID, targetID, idempotencyKey, amountMinutes, expectation)
}

func (uc *WorldUsecase) Seize(ctx context.Context, roleID, targetID, idempotencyKey string, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.Seize(roleID, targetID, idempotencyKey, expectation)
}

func (uc *WorldUsecase) RequestConversation(ctx context.Context, roleID, targetID, idempotencyKey string, expectation CommandExpectation) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	return uc.authority.RequestConversation(roleID, targetID, idempotencyKey, expectation)
}

func (uc *WorldUsecase) RespondConversation(ctx context.Context, roleID, conversationID, idempotencyKey, action string, expectation CommandExpectation) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	return uc.authority.RespondConversation(roleID, conversationID, idempotencyKey, action, expectation)
}

func (uc *WorldUsecase) SendConversationMessage(ctx context.Context, roleID, conversationID, idempotencyKey, content string, expectation CommandExpectation) (ConversationMessage, error) {
	if err := ctx.Err(); err != nil {
		return ConversationMessage{}, err
	}
	return uc.authority.SendConversationMessage(roleID, conversationID, idempotencyKey, content, expectation)
}

func (uc *WorldUsecase) CloseConversation(ctx context.Context, roleID, conversationID, idempotencyKey string, expectation CommandExpectation) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	return uc.authority.CloseConversation(roleID, conversationID, idempotencyKey, expectation)
}

func (uc *WorldUsecase) Conversations(ctx context.Context, roleID string) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return uc.authority.Conversations(roleID)
}

func (uc *WorldUsecase) Events(ctx context.Context, roleID string, after int64, limit int) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return uc.authority.Events(roleID, after, limit)
}

func (uc *WorldUsecase) Bounds(ctx context.Context) (Bounds, error) {
	if err := ctx.Err(); err != nil {
		return Bounds{}, err
	}
	return uc.authority.Bounds(), nil
}

func (uc *WorldUsecase) Reincarnate(ctx context.Context, roleID, idempotencyKey string, position *rules.Position, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.Reincarnate(roleID, idempotencyKey, position, expectation)
}

func (uc *WorldUsecase) logFailure(ctx context.Context, operation string, err error) {
	if err != nil {
		uc.log.WithContext(ctx).Warnw("operation", operation, "error", err)
	}
}
