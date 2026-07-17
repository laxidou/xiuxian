package rpc

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"

	worldv1 "xiuxian/gen/go/xiuxian/v1"
	"xiuxian/internal/rules"
	"xiuxian/internal/world"
)

type Server struct {
	worldv1.UnimplementedWorldServiceServer
	service *world.Service
}

func NewServer(service *world.Service) *Server { return &Server{service: service} }

func (s *Server) GetState(ctx context.Context, _ *worldv1.GetStateRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.service.State(roleID)
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *Server) GetWorldBounds(ctx context.Context, _ *worldv1.GetWorldBoundsRequest) (*worldv1.WorldBounds, error) {
	if _, err := s.authenticate(ctx); err != nil {
		return nil, err
	}
	bounds := s.service.Bounds()
	return WorldBounds(bounds), nil
}

func (s *Server) Scan(ctx context.Context, _ *worldv1.ScanRequest) (*worldv1.ScanResponse, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.service.Scan(roleID)
	if err != nil {
		return nil, mapError(err)
	}
	response := &worldv1.ScanResponse{HasMore: result.HasMore}
	for _, role := range result.Roles {
		entry := &worldv1.ScanRole{Id: role.ID, Name: role.Name, Realm: role.Realm, Status: string(role.Status), Distance: role.Distance}
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

func (s *Server) Move(ctx context.Context, request *worldv1.MoveRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if request.Target == nil {
		return nil, grpcstatus.Error(codes.InvalidArgument, "target is required")
	}
	state, err := s.service.Move(roleID, request.IdempotencyKey, rules.Position{X: rules.Coordinate(request.Target.XMilliunits), Y: rules.Coordinate(request.Target.YMilliunits)})
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *Server) Stop(ctx context.Context, request *worldv1.StopRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.service.Stop(roleID, request.IdempotencyKey)
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *Server) TransferCultivation(ctx context.Context, request *worldv1.TransferCultivationRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.service.Transfer(roleID, request.TargetId, request.IdempotencyKey, request.AmountMinutes)
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *Server) SeizeCultivation(ctx context.Context, request *worldv1.SeizeCultivationRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.service.Seize(roleID, request.TargetId, request.IdempotencyKey)
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *Server) Reincarnate(ctx context.Context, request *worldv1.ReincarnateRequest) (*worldv1.RoleState, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	var position *rules.Position
	if !request.Random && request.Position != nil {
		value := rules.Position{X: rules.Coordinate(request.Position.XMilliunits), Y: rules.Coordinate(request.Position.YMilliunits)}
		position = &value
	}
	state, err := s.service.Reincarnate(roleID, request.IdempotencyKey, position)
	if err != nil {
		return nil, mapError(err)
	}
	return RoleState(state), nil
}

func (s *Server) ListRecentEvents(ctx context.Context, request *worldv1.ListRecentEventsRequest) (*worldv1.ListRecentEventsResponse, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	events, err := s.service.Events(roleID, request.After, int(request.Limit))
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

func (s *Server) ListConversations(ctx context.Context, _ *worldv1.ListConversationsRequest) (*worldv1.ListConversationsResponse, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.service.Conversations(roleID)
	if err != nil {
		return nil, mapError(err)
	}
	response := &worldv1.ListConversationsResponse{}
	for _, item := range items {
		response.Conversations = append(response.Conversations, conversation(item))
	}
	return response, nil
}

func (s *Server) RequestConversation(ctx context.Context, request *worldv1.RequestConversationRequest) (*worldv1.Conversation, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.service.RequestConversation(roleID, request.TargetId, request.IdempotencyKey)
	if err != nil {
		return nil, mapError(err)
	}
	return conversation(result), nil
}

func (s *Server) RespondConversation(ctx context.Context, request *worldv1.RespondConversationRequest) (*worldv1.Conversation, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.service.RespondConversation(roleID, request.ConversationId, request.IdempotencyKey, request.Action)
	if err != nil {
		return nil, mapError(err)
	}
	return conversation(result), nil
}

func (s *Server) SendConversationMessage(ctx context.Context, request *worldv1.SendConversationMessageRequest) (*worldv1.ConversationMessage, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.service.SendConversationMessage(roleID, request.ConversationId, request.IdempotencyKey, request.Content)
	if err != nil {
		return nil, mapError(err)
	}
	return conversationMessage(result), nil
}

func (s *Server) CloseConversation(ctx context.Context, request *worldv1.CloseConversationRequest) (*worldv1.Conversation, error) {
	roleID, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.service.CloseConversation(roleID, request.ConversationId, request.IdempotencyKey)
	if err != nil {
		return nil, mapError(err)
	}
	return conversation(result), nil
}

func (s *Server) authenticate(ctx context.Context) (string, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", grpcstatus.Error(codes.Unauthenticated, "role API Key required")
	}
	roleID, err := s.service.AuthenticateAPIKey(strings.TrimPrefix(values[0], "Bearer "))
	if err != nil {
		return "", grpcstatus.Error(codes.Unauthenticated, err.Error())
	}
	return roleID, nil
}

func RoleState(state world.State) *worldv1.RoleState {
	return &worldv1.RoleState{Id: state.ID, Name: state.Name, LifeNumber: state.LifeNumber, Status: string(state.Status), CultivationMillis: int64(math.Round(state.Cultivation * 60000)), RealmLevel: int32(state.RealmLevel), RealmName: state.Realm, AgeMillis: int64(math.Round(state.AgeSeconds * 1000)), LifespanMillis: int64(math.Round(state.LifespanSeconds * 1000)), Speed: state.Speed, SenseRadius: state.SenseRadius, Position: protoPosition(state.Position), MovementState: string(state.MovementState), StateVersion: state.StateVersion}
}

func WorldBounds(bounds world.Bounds) *worldv1.WorldBounds {
	return &worldv1.WorldBounds{MinXMilliunits: milliunits(bounds.MinX), MaxXMilliunits: milliunits(bounds.MaxX), MinYMilliunits: milliunits(bounds.MinY), MaxYMilliunits: milliunits(bounds.MaxY)}
}

func protoPosition(position world.PublicPosition) *worldv1.Position {
	return &worldv1.Position{XMilliunits: milliunits(position.X), YMilliunits: milliunits(position.Y)}
}
func milliunits(value float64) int64 { return int64(math.Round(value * 1000)) }

func conversation(value world.Conversation) *worldv1.Conversation {
	result := &worldv1.Conversation{Id: value.ID, RequesterId: value.RequesterID, RecipientId: value.RecipientID, Status: string(value.Status), CreatedAtUnixMillis: value.CreatedAt, UpdatedAtUnixMillis: value.UpdatedAt}
	for _, message := range value.Messages {
		result.Messages = append(result.Messages, conversationMessage(message))
	}
	return result
}

func conversationMessage(value world.ConversationMessage) *worldv1.ConversationMessage {
	return &worldv1.ConversationMessage{Id: value.ID, SenderId: value.SenderID, Content: value.Content, Trusted: value.Trusted, CreatedAtUnixMillis: value.CreatedAt}
}

func mapError(err error) error {
	switch err {
	case world.ErrInvalid, world.ErrIdempotencyKey:
		return grpcstatus.Error(codes.InvalidArgument, err.Error())
	case world.ErrUnauthenticated:
		return grpcstatus.Error(codes.Unauthenticated, err.Error())
	case world.ErrNotFound:
		return grpcstatus.Error(codes.NotFound, err.Error())
	case world.ErrForbidden:
		return grpcstatus.Error(codes.PermissionDenied, err.Error())
	case world.ErrConflict, world.ErrNotAlive:
		return grpcstatus.Error(codes.FailedPrecondition, err.Error())
	case world.ErrRateLimited:
		return grpcstatus.Error(codes.ResourceExhausted, err.Error())
	default:
		return grpcstatus.Error(codes.Internal, "internal error")
	}
}
