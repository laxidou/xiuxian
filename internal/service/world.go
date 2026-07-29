package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/go-kratos/kratos/v2/transport"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	"google.golang.org/grpc/metadata"

	worldv1 "xiuxian/gen/go/xiuxian/v1"
	"xiuxian/internal/biz"
	"xiuxian/internal/rules"
)

type WorldService struct {
	worldv1.UnimplementedWorldServiceServer
	usecase *biz.WorldUsecase
	limiter biz.RateLimiter
}

func (s *WorldService) GetGameRules(ctx context.Context, _ *worldv1.GetGameRulesRequest) (*worldv1.GameRules, error) {
	guide, err := s.usecase.Rules(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	response := &worldv1.GameRules{RuleVersion: guide.RuleVersion, Title: guide.Title, Summary: guide.Summary, AiRules: guide.AIRules, CanonicalUrl: guide.CanonicalURL}
	for _, section := range guide.Sections {
		response.Sections = append(response.Sections, &worldv1.GameRuleSection{Id: section.ID, Title: section.Title, Body: section.Body})
	}
	for _, realm := range guide.Realms {
		response.Realms = append(response.Realms, &worldv1.RealmRule{Level: int32(realm.Level), Name: realm.Name, CultivationThresholdMillis: realm.CultivationThresholdMillis, LifespanMillis: realm.LifespanMillis, Speed: realm.Speed, SenseRadius: realm.SenseRadius})
	}
	return response, nil
}

func NewWorldService(usecase *biz.WorldUsecase, limiter biz.RateLimiter) *WorldService {
	return &WorldService{usecase: usecase, limiter: limiter}
}

func (s *WorldService) GetState(ctx context.Context, _ *worldv1.GetStateRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.usecase.State(ctx, roleID)
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *WorldService) GetWorldBounds(ctx context.Context, _ *worldv1.GetWorldBoundsRequest) (*worldv1.WorldBounds, error) {
	if _, err := s.authenticate(ctx); err != nil {
		return nil, err
	}
	bounds, err := s.usecase.Bounds(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return WorldBounds(bounds), nil
}

func (s *WorldService) Scan(ctx context.Context, request *worldv1.ScanRequest) (*worldv1.ScanResponse, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.usecase.Scan(ctx, roleID, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	response := &worldv1.ScanResponse{
		HasMore:                result.HasMore,
		TruncatedRoles:         int32(result.TruncatedRoles),
		TruncatedOpportunities: int32(result.TruncatedOpportunities),
	}
	for _, role := range result.Roles {
		entry := &worldv1.ScanRole{
			Id: role.ID, Name: role.Name, Realm: role.Realm, Status: string(role.Status), Distance: role.Distance,
			CanTransfer: role.CanTransfer, CanSeize: role.CanSeize, CanRequestConversation: role.CanRequestConversation,
		}
		if role.Position != nil {
			entry.Position = protoPosition(*role.Position)
		}
		response.Roles = append(response.Roles, entry)
	}
	for _, opportunity := range result.Opportunities {
		response.Opportunities = append(response.Opportunities, &worldv1.OpportunitySignal{Message: opportunity.Message, Distance: opportunity.Distance})
	}
	return response, nil
}

func (s *WorldService) Move(ctx context.Context, request *worldv1.MoveRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if request.Target == nil {
		return nil, ErrorTargetRequired("target is required")
	}
	state, err := s.usecase.Move(ctx, roleID, request.IdempotencyKey, rules.Position{X: rules.Coordinate(request.Target.XMilliunits), Y: rules.Coordinate(request.Target.YMilliunits)}, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *WorldService) MoveDirection(ctx context.Context, request *worldv1.MoveDirectionRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.usecase.MoveDirection(ctx, roleID, request.IdempotencyKey, directionFromProto(request.Direction), request.Speed, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *WorldService) Stop(ctx context.Context, request *worldv1.StopRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.usecase.Stop(ctx, roleID, request.IdempotencyKey, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *WorldService) TransferCultivation(ctx context.Context, request *worldv1.TransferCultivationRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.usecase.Transfer(ctx, roleID, request.TargetId, request.IdempotencyKey, request.AmountMinutes, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *WorldService) SeizeCultivation(ctx context.Context, request *worldv1.SeizeCultivationRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.usecase.Seize(ctx, roleID, request.TargetId, request.IdempotencyKey, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *WorldService) Reincarnate(ctx context.Context, request *worldv1.ReincarnateRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	var position *rules.Position
	if !request.Random && request.Position != nil {
		value := rules.Position{X: rules.Coordinate(request.Position.XMilliunits), Y: rules.Coordinate(request.Position.YMilliunits)}
		position = &value
	}
	state, err := s.usecase.Reincarnate(ctx, roleID, request.IdempotencyKey, position, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *WorldService) ListRecentEvents(ctx context.Context, request *worldv1.ListRecentEventsRequest) (*worldv1.ListRecentEventsResponse, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	events, err := s.usecase.Events(ctx, roleID, request.After, int(request.Limit))
	if err != nil {
		return nil, mapError(err)
	}
	response := &worldv1.ListRecentEventsResponse{}
	for _, event := range events {
		data, _ := json.Marshal(event.Data)
		response.Events = append(response.Events, &worldv1.WorldEvent{Id: event.ID, Type: string(event.Type), Message: event.Message, CreatedAtUnixMillis: event.CreatedAt, LifeNumber: event.LifeNumber, DataJson: string(data)})
	}
	return response, nil
}

func (s *WorldService) ListConversations(ctx context.Context, _ *worldv1.ListConversationsRequest) (*worldv1.ListConversationsResponse, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.usecase.Conversations(ctx, roleID)
	if err != nil {
		return nil, mapError(err)
	}
	response := &worldv1.ListConversationsResponse{}
	for _, item := range items {
		response.Conversations = append(response.Conversations, conversation(item))
	}
	return response, nil
}

func (s *WorldService) RequestConversation(ctx context.Context, request *worldv1.RequestConversationRequest) (*worldv1.Conversation, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.usecase.RequestConversation(ctx, roleID, request.TargetId, request.IdempotencyKey, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return conversation(result), nil
}

func (s *WorldService) RespondConversation(ctx context.Context, request *worldv1.RespondConversationRequest) (*worldv1.Conversation, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.usecase.RespondConversation(ctx, roleID, request.ConversationId, request.IdempotencyKey, request.Action, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return conversation(result), nil
}

func (s *WorldService) SendConversationMessage(ctx context.Context, request *worldv1.SendConversationMessageRequest) (*worldv1.ConversationMessage, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.usecase.SendConversationMessage(ctx, roleID, request.ConversationId, request.IdempotencyKey, request.Content, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return conversationMessage(result), nil
}

func (s *WorldService) CloseConversation(ctx context.Context, request *worldv1.CloseConversationRequest) (*worldv1.Conversation, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.usecase.CloseConversation(ctx, roleID, request.ConversationId, request.IdempotencyKey, commandExpectation(request.ExpectedLifeNumber, request.ExpectedStateVersion))
	if err != nil {
		return nil, mapError(err)
	}
	return conversation(result), nil
}

func (s *WorldService) authenticate(ctx context.Context) (string, error) {
	tr, ok := transport.FromServerContext(ctx)
	authorization := ""
	if ok {
		authorization = tr.RequestHeader().Get("authorization")
	} else if values := metadata.ValueFromIncomingContext(ctx, "authorization"); len(values) == 1 {
		authorization = values[0]
	}
	if strings.HasPrefix(authorization, "Bearer ") {
		key := strings.TrimPrefix(authorization, "Bearer ")
		roleID, err := s.usecase.AuthenticateAPIKey(ctx, key)
		if err == nil {
			if err := s.enforceRateLimit(ctx, "api_key", key, biz.APIKeyRateLimit); err != nil {
				return "", err
			}
			return roleID, nil
		}
	}
	if request, ok := kratoshttp.RequestFromServerContext(ctx); ok {
		if cookie, err := request.Cookie("xiuxian_session"); err == nil {
			roleID, authErr := s.usecase.AuthenticateSession(ctx, cookie.Value)
			if authErr == nil {
				if err := s.enforceRateLimit(ctx, "web_session", cookie.Value, biz.WebSessionRateLimit); err != nil {
					return "", err
				}
				return roleID, nil
			}
		}
	}
	return "", ErrorUnauthorized("authentication required")
}

func (s *WorldService) enforceRateLimit(ctx context.Context, scope, subject string, policy biz.RateLimitPolicy) error {
	allowed, err := s.limiter.Allow(ctx, scope, subject, policy)
	if err != nil {
		return ErrorRateLimitUnavailable("rate limiter unavailable")
	}
	if !allowed {
		return ErrorRateLimited("rate limit exceeded")
	}
	return nil
}

func RoleState(state biz.State) *worldv1.RoleState {
	return &worldv1.RoleState{Id: state.ID, Name: state.Name, LifeNumber: state.LifeNumber, Status: string(state.Status), CultivationMillis: int64(math.Round(state.Cultivation * 60000)), RealmLevel: int32(state.RealmLevel), RealmName: state.Realm, AgeMillis: int64(math.Round(state.AgeSeconds * 1000)), LifespanMillis: int64(math.Round(state.LifespanSeconds * 1000)), Speed: state.Speed, SenseRadius: state.SenseRadius, Position: protoPosition(state.Position), MovementState: string(state.MovementState), StateVersion: state.StateVersion, RuleVersion: state.RuleVersion, MovementMode: state.MovementMode, MovementDirection: state.MovementDirection, MovementSpeedSetting: state.MovementSpeedSetting, ActualMovementSpeed: state.ActualMovementSpeed}
}

func WorldBounds(bounds biz.Bounds) *worldv1.WorldBounds {
	return &worldv1.WorldBounds{MinXMilliunits: milliunits(bounds.MinX), MaxXMilliunits: milliunits(bounds.MaxX), MinYMilliunits: milliunits(bounds.MinY), MaxYMilliunits: milliunits(bounds.MaxY)}
}

func protoPosition(position biz.PublicPosition) *worldv1.Position {
	return &worldv1.Position{XMilliunits: milliunits(position.X), YMilliunits: milliunits(position.Y)}
}
func milliunits(value float64) int64 { return int64(math.Round(value * 1000)) }

func directionFromProto(direction worldv1.Direction) rules.Direction {
	switch direction {
	case worldv1.Direction_DIRECTION_UP:
		return rules.DirectionUp
	case worldv1.Direction_DIRECTION_DOWN:
		return rules.DirectionDown
	case worldv1.Direction_DIRECTION_LEFT:
		return rules.DirectionLeft
	case worldv1.Direction_DIRECTION_RIGHT:
		return rules.DirectionRight
	default:
		return ""
	}
}

func conversation(value biz.Conversation) *worldv1.Conversation {
	result := &worldv1.Conversation{Id: value.ID, RequesterId: value.RequesterID, RecipientId: value.RecipientID, Status: string(value.Status), CreatedAtUnixMillis: value.CreatedAt, UpdatedAtUnixMillis: value.UpdatedAt}
	for _, message := range value.Messages {
		result.Messages = append(result.Messages, conversationMessage(message))
	}
	return result
}

func conversationMessage(value biz.ConversationMessage) *worldv1.ConversationMessage {
	return &worldv1.ConversationMessage{Id: value.ID, SenderId: value.SenderID, Content: value.Content, Trusted: value.Trusted, CreatedAtUnixMillis: value.CreatedAt}
}

func commandExpectation(lifeNumber, stateVersion int64) biz.CommandExpectation {
	return biz.CommandExpectation{LifeNumber: lifeNumber, StateVersion: stateVersion}
}

func mapError(err error) error {
	switch {
	case errors.Is(err, biz.ErrInvalid), errors.Is(err, biz.ErrIdempotencyKey):
		return ErrorBadRequest(err.Error())
	case errors.Is(err, biz.ErrUnauthenticated):
		return ErrorUnauthorized(err.Error())
	case errors.Is(err, biz.ErrNotFound):
		return ErrorNotFound(err.Error())
	case errors.Is(err, biz.ErrForbidden), errors.Is(err, biz.ErrTargetIneligible):
		return ErrorForbidden(err.Error())
	case errors.Is(err, biz.ErrConflict), errors.Is(err, biz.ErrNotAlive), errors.Is(err, biz.ErrStaleCommand):
		return ErrorPreconditionFailed(err.Error())
	case errors.Is(err, biz.ErrRateLimited):
		return ErrorRateLimited(err.Error())
	default:
		return ErrorInternal("internal error")
	}
}
