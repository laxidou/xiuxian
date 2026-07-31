package biz

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"xiuxian/internal/rules"
)

// WorldRepository is the persistence contract required by the world authority.
// It is defined in the biz layer so infrastructure remains replaceable.
type WorldRepository interface {
	Load(context.Context) ([]byte, error)
	Save(context.Context, []byte) error
}

// WorldAuthority captures the domain operations consumed by application use
// cases.
type WorldAuthority interface {
	Clock() Clock
	Rules(context.Context) (GameRules, error)
	Register(context.Context, string, string, string) (string, State, error)
	Login(context.Context, string, string) (string, State, error)
	AuthenticateSession(context.Context, string) (string, error)
	Logout(context.Context, string) error
	AuthenticateAPIKey(context.Context, string) (string, error)
	RotateMCPKey(context.Context, string) (string, error)
	RevokeMCPKey(context.Context, string) error
	State(context.Context, string) (State, error)
	SettleDeadline(context.Context, string, int64) (bool, error)
	Move(context.Context, string, string, rules.Position, CommandExpectation) (State, error)
	MoveDirection(context.Context, string, string, rules.Direction, int64, CommandExpectation) (State, error)
	Stop(context.Context, string, string, CommandExpectation) (State, error)
	Scan(context.Context, string, CommandExpectation) (ScanResult, error)
	Transfer(context.Context, string, string, string, int64, CommandExpectation) (State, error)
	Seize(context.Context, string, string, string, CommandExpectation) (State, error)
	RequestConversation(context.Context, string, string, string, CommandExpectation) (Conversation, error)
	RespondConversation(context.Context, string, string, string, string, CommandExpectation) (Conversation, error)
	SendConversationMessage(context.Context, string, string, string, string, CommandExpectation) (ConversationMessage, error)
	CloseConversation(context.Context, string, string, string, CommandExpectation) (Conversation, error)
	Conversations(context.Context, string) ([]Conversation, error)
	Events(context.Context, string, int64, int) ([]Event, error)
	Bounds(context.Context) (Bounds, error)
}

type repositoryWithoutAuthorityTime struct {
	WorldRepository
}

func NewWorldAuthority(repository WorldRepository, clock Clock) (*Service, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if clock == nil {
		clock = SystemClock{}
	}
	store := repository
	if _, ok := clock.(*ManualClock); ok {
		store = repositoryWithoutAuthorityTime{WorldRepository: repository}
	}
	return NewPersistentService(ctx, clock, store)
}

// WorldUsecase is the application boundary shared by HTTP and gRPC transports.
type WorldUsecase struct {
	authority WorldAuthority
	log       *log.Helper
}

func NewWorldUsecase(authority WorldAuthority, logger log.Logger) *WorldUsecase {
	return &WorldUsecase{authority: authority, log: log.NewHelper(logger)}
}

func (uc *WorldUsecase) Clock() Clock { return uc.authority.Clock() }

func (uc *WorldUsecase) Rules(ctx context.Context) (GameRules, error) {
	if err := ctx.Err(); err != nil {
		return GameRules{}, err
	}
	return uc.authority.Rules(ctx)
}

func (uc *WorldUsecase) Register(ctx context.Context, account, password, roleName string) (string, State, error) {
	if err := ctx.Err(); err != nil {
		return "", State{}, err
	}
	token, state, err := uc.authority.Register(ctx, account, password, roleName)
	uc.logFailure(ctx, "register", err)
	return token, state, err
}

func (uc *WorldUsecase) Login(ctx context.Context, account, password string) (string, State, error) {
	if err := ctx.Err(); err != nil {
		return "", State{}, err
	}
	token, state, err := uc.authority.Login(ctx, account, password)
	uc.logFailure(ctx, "login", err)
	return token, state, err
}

func (uc *WorldUsecase) AuthenticateSession(ctx context.Context, token string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return uc.authority.AuthenticateSession(ctx, token)
}

func (uc *WorldUsecase) Logout(ctx context.Context, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return uc.authority.Logout(ctx, token)
}

func (uc *WorldUsecase) AuthenticateAPIKey(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return uc.authority.AuthenticateAPIKey(ctx, key)
}

func (uc *WorldUsecase) RotateMCPKey(ctx context.Context, roleID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return uc.authority.RotateMCPKey(ctx, roleID)
}

func (uc *WorldUsecase) RevokeMCPKey(ctx context.Context, roleID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return uc.authority.RevokeMCPKey(ctx, roleID)
}

func (uc *WorldUsecase) State(ctx context.Context, roleID string) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.State(ctx, roleID)
}

func (uc *WorldUsecase) SettleDeadline(ctx context.Context, roleID string, expectedVersion int64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return uc.authority.SettleDeadline(ctx, roleID, expectedVersion)
}

func (uc *WorldUsecase) Move(ctx context.Context, roleID, idempotencyKey string, target rules.Position, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.Move(ctx, roleID, idempotencyKey, target, expectation)
}

func (uc *WorldUsecase) MoveDirection(ctx context.Context, roleID, idempotencyKey string, direction rules.Direction, speed int64, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.MoveDirection(ctx, roleID, idempotencyKey, direction, speed, expectation)
}

func (uc *WorldUsecase) Stop(ctx context.Context, roleID, idempotencyKey string, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.Stop(ctx, roleID, idempotencyKey, expectation)
}

func (uc *WorldUsecase) Scan(ctx context.Context, roleID string, expectation CommandExpectation) (ScanResult, error) {
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}
	return uc.authority.Scan(ctx, roleID, expectation)
}

func (uc *WorldUsecase) Transfer(ctx context.Context, roleID, targetID, idempotencyKey string, amountMinutes int64, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.Transfer(ctx, roleID, targetID, idempotencyKey, amountMinutes, expectation)
}

func (uc *WorldUsecase) Seize(ctx context.Context, roleID, targetID, idempotencyKey string, expectation CommandExpectation) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	return uc.authority.Seize(ctx, roleID, targetID, idempotencyKey, expectation)
}

func (uc *WorldUsecase) RequestConversation(ctx context.Context, roleID, targetID, idempotencyKey string, expectation CommandExpectation) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	return uc.authority.RequestConversation(ctx, roleID, targetID, idempotencyKey, expectation)
}

func (uc *WorldUsecase) RespondConversation(ctx context.Context, roleID, conversationID, idempotencyKey, action string, expectation CommandExpectation) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	return uc.authority.RespondConversation(ctx, roleID, conversationID, idempotencyKey, action, expectation)
}

func (uc *WorldUsecase) SendConversationMessage(ctx context.Context, roleID, conversationID, idempotencyKey, content string, expectation CommandExpectation) (ConversationMessage, error) {
	if err := ctx.Err(); err != nil {
		return ConversationMessage{}, err
	}
	return uc.authority.SendConversationMessage(ctx, roleID, conversationID, idempotencyKey, content, expectation)
}

func (uc *WorldUsecase) CloseConversation(ctx context.Context, roleID, conversationID, idempotencyKey string, expectation CommandExpectation) (Conversation, error) {
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	return uc.authority.CloseConversation(ctx, roleID, conversationID, idempotencyKey, expectation)
}

func (uc *WorldUsecase) Conversations(ctx context.Context, roleID string) ([]Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return uc.authority.Conversations(ctx, roleID)
}

func (uc *WorldUsecase) Events(ctx context.Context, roleID string, after int64, limit int) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return uc.authority.Events(ctx, roleID, after, limit)
}

func (uc *WorldUsecase) Bounds(ctx context.Context) (Bounds, error) {
	if err := ctx.Err(); err != nil {
		return Bounds{}, err
	}
	return uc.authority.Bounds(ctx)
}

func (uc *WorldUsecase) logFailure(ctx context.Context, operation string, err error) {
	if err != nil {
		uc.log.WithContext(ctx).Warnw("operation", operation, "error", err)
	}
}
