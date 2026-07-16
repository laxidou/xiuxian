package world

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"xiuxian/internal/rules"
)

var (
	ErrConflict        = errors.New("resource already exists")
	ErrInvalid         = errors.New("invalid request")
	ErrUnauthenticated = errors.New("authentication required")
	ErrNotAlive        = errors.New("role is not alive")
	ErrIdempotencyKey  = errors.New("idempotency key is required")
	ErrNotFound        = errors.New("resource not found")
	ErrForbidden       = errors.New("action is not allowed")
	ErrRateLimited     = errors.New("rate limit exceeded")
)

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type ManualClock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewManualClock(now time.Time) *ManualClock {
	return &ManualClock{now: now.UTC()}
}

func (c *ManualClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *ManualClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

type Role struct {
	ID                    string
	Account               string
	Name                  string
	LifeNumber            int64
	Status                string
	LifeStartedAt         time.Time
	CultivationBase       rules.Cultivation
	CultivationAt         time.Time
	LastSettledAt         time.Time
	Position              rules.Position
	Trajectory            *rules.Trajectory
	TrajectoryCultivation rules.Cultivation
	StateVersion          int64
	NextDeathAt           time.Time
	LastScanAt            time.Time
	MCPKeyHash            [32]byte
	BoundOpportunityID    string
}

type State struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	LifeNumber      int64          `json:"life_number"`
	Status          string         `json:"status"`
	Cultivation     float64        `json:"cultivation"`
	RealmLevel      int            `json:"realm_level"`
	Realm           string         `json:"realm"`
	AgeSeconds      float64        `json:"age_seconds"`
	LifespanSeconds float64        `json:"lifespan_seconds"`
	Speed           int64          `json:"speed"`
	SenseRadius     int64          `json:"sense_radius"`
	Position        PublicPosition `json:"position"`
	MovementState   string         `json:"movement_state"`
	StateVersion    int64          `json:"state_version"`
}

type PublicPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type account struct {
	passwordHash []byte
	roleID       string
}

type session struct {
	RoleID    string
	ExpiresAt time.Time
}

type Event struct {
	ID         int64          `json:"id"`
	Type       string         `json:"type"`
	Message    string         `json:"message"`
	CreatedAt  int64          `json:"created_at"`
	LifeNumber int64          `json:"life_number"`
	Data       map[string]any `json:"data,omitempty"`
}

type Bounds struct {
	MinX float64 `json:"min_x"`
	MaxX float64 `json:"max_x"`
	MinY float64 `json:"min_y"`
	MaxY float64 `json:"max_y"`
}

type Opportunity struct {
	ID          string
	Position    rules.Position
	Cultivation rules.Cultivation
	SenseRadius rules.Coordinate
	Status      string
	BoundRoleID string
	BoundAt     time.Time
	Credited    rules.Cultivation
}

type ScanRole struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Realm    string          `json:"realm"`
	Status   string          `json:"status"`
	Distance float64         `json:"distance"`
	Position *PublicPosition `json:"position,omitempty"`
}

type OpportunitySignal struct {
	Message  string  `json:"message"`
	Distance float64 `json:"distance"`
}

type ScanResult struct {
	Roles         []ScanRole          `json:"roles"`
	Opportunities []OpportunitySignal `json:"opportunities"`
	HasMore       bool                `json:"has_more"`
}

type ConversationMessage struct {
	ID        int64  `json:"id"`
	SenderID  string `json:"sender_id"`
	Content   string `json:"content"`
	Trusted   bool   `json:"trusted"`
	CreatedAt int64  `json:"created_at"`
}

type Conversation struct {
	ID          string                `json:"id"`
	RequesterID string                `json:"requester_id"`
	RecipientID string                `json:"recipient_id"`
	Status      string                `json:"status"`
	Messages    []ConversationMessage `json:"messages"`
	CreatedAt   int64                 `json:"created_at"`
	UpdatedAt   int64                 `json:"updated_at"`
}

type Service struct {
	mu                  sync.Mutex
	clock               Clock
	accounts            map[string]account
	roleNames           map[string]string
	roles               map[string]*Role
	sessions            map[[32]byte]session
	idempotency         map[string]map[string]State
	events              map[string][]Event
	opportunities       map[string]*Opportunity
	conversations       map[string]*Conversation
	conversationResults map[string]string
	eventSequence       int64
	minX                rules.Coordinate
	maxX                rules.Coordinate
	minY                rules.Coordinate
	maxY                rules.Coordinate
	nextID              uint64
	store               DurableStore
}

type DurableStore interface {
	Load(context.Context) ([]byte, error)
	Save(context.Context, []byte) error
}

func NewService(clock Clock) *Service {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Service{
		clock:               clock,
		accounts:            make(map[string]account),
		roleNames:           make(map[string]string),
		roles:               make(map[string]*Role),
		sessions:            make(map[[32]byte]session),
		idempotency:         make(map[string]map[string]State),
		events:              make(map[string][]Event),
		opportunities:       make(map[string]*Opportunity),
		conversations:       make(map[string]*Conversation),
		conversationResults: make(map[string]string),
	}
}

func NewPersistentService(ctx context.Context, clock Clock, store DurableStore) (*Service, error) {
	service := NewService(clock)
	service.store = store
	payload, err := store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load world snapshot: %w", err)
	}
	if len(payload) > 0 {
		if err := service.restoreLocked(payload); err != nil {
			return nil, fmt.Errorf("restore world snapshot: %w", err)
		}
	}
	return service, nil
}

func (s *Service) Clock() Clock { return s.clock }

func (s *Service) Register(accountName, password, roleName string) (string, State, error) {
	accountName = strings.TrimSpace(accountName)
	roleName = strings.TrimSpace(roleName)
	if accountName == "" || roleName == "" || len(password) < 12 || len(accountName) > 128 || len(roleName) > 64 {
		return "", State{}, ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", State{}, fmt.Errorf("hash password: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounts[accountName]; exists {
		return "", State{}, ErrConflict
	}
	if _, exists := s.roleNames[roleName]; exists {
		return "", State{}, ErrConflict
	}

	now := s.authoritativeNowLocked(nil)
	s.nextID++
	roleID := fmt.Sprintf("role_%d", s.nextID)
	role := &Role{
		ID:            roleID,
		Account:       accountName,
		Name:          roleName,
		LifeNumber:    1,
		Status:        "alive",
		LifeStartedAt: now,
		CultivationAt: now,
		LastSettledAt: now,
		Position:      rules.Position{},
		StateVersion:  1,
		NextDeathAt:   now.Add(rules.NextNaturalDeathAfter(0, 0)),
	}
	s.accounts[accountName] = account{passwordHash: hash, roleID: roleID}
	s.roleNames[roleName] = roleID
	s.roles[roleID] = role
	token, tokenHash, err := newToken()
	if err != nil {
		return "", State{}, err
	}
	s.sessions[tokenHash] = session{RoleID: roleID, ExpiresAt: now.Add(24 * time.Hour)}
	state := s.stateLocked(role, now)
	if err := s.persistLocked(); err != nil {
		return "", State{}, err
	}
	return token, state, nil
}

func (s *Service) Login(accountName, password string) (string, State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.accounts[strings.TrimSpace(accountName)]
	if !ok || bcrypt.CompareHashAndPassword(entry.passwordHash, []byte(password)) != nil {
		return "", State{}, ErrUnauthenticated
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return "", State{}, err
	}
	role := s.roles[entry.roleID]
	now := s.authoritativeNowLocked(role)
	s.sessions[tokenHash] = session{RoleID: entry.roleID, ExpiresAt: now.Add(24 * time.Hour)}
	state := s.stateLocked(role, now)
	if err := s.persistLocked(); err != nil {
		return "", State{}, err
	}
	return token, state, nil
}

func (s *Service) AuthenticateSession(token string) (string, error) {
	if token == "" {
		return "", ErrUnauthenticated
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := sha256.Sum256([]byte(token))
	value, ok := s.sessions[hash]
	if !ok || !s.clock.Now().Before(value.ExpiresAt) {
		delete(s.sessions, hash)
		return "", ErrUnauthenticated
	}
	return value.RoleID, nil
}

func (s *Service) Logout(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sha256.Sum256([]byte(token)))
	return s.persistLocked()
}

func (s *Service) AuthenticateAPIKey(key string) (string, error) {
	if key == "" {
		return "", ErrUnauthenticated
	}
	hash := sha256.Sum256([]byte(key))
	s.mu.Lock()
	defer s.mu.Unlock()
	for roleID, role := range s.roles {
		if role.MCPKeyHash == hash {
			return roleID, nil
		}
	}
	return "", ErrUnauthenticated
}

func (s *Service) RotateMCPKey(roleID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[roleID]
	if !ok {
		return "", ErrUnauthenticated
	}
	token, _, err := newToken()
	if err != nil {
		return "", err
	}
	key := "xiu_" + token
	role.MCPKeyHash = sha256.Sum256([]byte(key))
	role.StateVersion++
	s.appendEventLocked(role, s.authoritativeNowLocked(role), "mcp_key_rotated", "MCP API Key 已轮换", nil)
	if err := s.persistLocked(); err != nil {
		return "", err
	}
	return key, nil
}

func (s *Service) RevokeMCPKey(roleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[roleID]
	if !ok {
		return ErrUnauthenticated
	}
	role.MCPKeyHash = [32]byte{}
	role.StateVersion++
	s.appendEventLocked(role, s.authoritativeNowLocked(role), "mcp_key_revoked", "MCP API Key 已撤销", nil)
	return s.persistLocked()
}

func (s *Service) State(roleID string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[roleID]
	if !ok {
		return State{}, ErrUnauthenticated
	}
	now := s.authoritativeNowLocked(role)
	state := s.stateLocked(role, now)
	if err := s.persistLocked(); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Service) Move(roleID, idempotencyKey string, target rules.Position) (State, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return State{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[roleID]
	if !ok {
		return State{}, ErrUnauthenticated
	}
	if previous, ok := s.idempotencyResultLocked(roleID, idempotencyKey); ok {
		return previous, nil
	}
	now := s.authoritativeNowLocked(role)
	current := s.stateLocked(role, now)
	if role.Status != "alive" {
		return State{}, ErrNotAlive
	}
	position := rules.Position{X: rules.Units(current.Position.X), Y: rules.Units(current.Position.Y)}
	role.Position = position
	realm := rules.RealmFor(s.cultivationLocked(role, now))
	role.Trajectory = &rules.Trajectory{Start: position, Target: target, StartedAt: now, Speed: realm.Speed}
	role.TrajectoryCultivation = s.cultivationLocked(role, now)
	role.StateVersion++
	result := s.stateLocked(role, now)
	s.rememberIdempotencyLocked(roleID, idempotencyKey, result)
	if err := s.persistLocked(); err != nil {
		return State{}, err
	}
	return result, nil
}

func (s *Service) Stop(roleID, idempotencyKey string) (State, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return State{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[roleID]
	if !ok {
		return State{}, ErrUnauthenticated
	}
	if previous, ok := s.idempotencyResultLocked(roleID, idempotencyKey); ok {
		return previous, nil
	}
	now := s.authoritativeNowLocked(role)
	current := s.stateLocked(role, now)
	if role.Status != "alive" {
		return State{}, ErrNotAlive
	}
	role.Position = positionOfState(current)
	role.Trajectory = nil
	role.TrajectoryCultivation = 0
	role.StateVersion++
	s.expandBoundsLocked(role.Position)
	result := s.stateLocked(role, now)
	s.rememberIdempotencyLocked(roleID, idempotencyKey, result)
	if err := s.persistLocked(); err != nil {
		return State{}, err
	}
	return result, nil
}

func (s *Service) Scan(roleID string) (ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scanner, ok := s.roles[roleID]
	if !ok {
		return ScanResult{}, ErrUnauthenticated
	}
	now := s.authoritativeNowLocked(scanner)
	scannerState := s.stateLocked(scanner, now)
	if scanner.Status != "alive" {
		return ScanResult{}, ErrNotAlive
	}
	if !scanner.LastScanAt.IsZero() && now.Sub(scanner.LastScanAt) < 5*time.Second {
		return ScanResult{}, ErrRateLimited
	}
	scanner.LastScanAt = now
	scannerPosition := positionOfState(scannerState)
	result := ScanResult{Roles: []ScanRole{}, Opportunities: []OpportunitySignal{}}
	for targetID, target := range s.roles {
		if targetID == roleID {
			continue
		}
		targetState := s.stateLocked(target, now)
		if target.Status != "alive" {
			continue
		}
		targetPosition := positionOfState(targetState)
		distance := rules.Distance(scannerPosition, targetPosition)
		if distance > float64(scannerState.SenseRadius) {
			continue
		}
		entry := ScanRole{ID: target.ID, Name: target.Name, Realm: targetState.Realm, Status: targetState.Status, Distance: distance}
		if scannerState.RealmLevel > targetState.RealmLevel {
			position := targetState.Position
			entry.Position = &position
			s.appendEventLocked(target, now, "scanned", "被更高境界角色神识扫描", map[string]any{
				"direction":    direction(targetPosition, scannerPosition),
				"scanner_name": scanner.Name,
			})
		}
		result.Roles = append(result.Roles, entry)
	}
	for _, opportunity := range s.opportunities {
		if opportunity.Status != "unclaimed" {
			continue
		}
		if rules.CanSenseOpportunity(scannerPosition, rules.Units(float64(scannerState.SenseRadius)), opportunity.Position, opportunity.SenseRadius) {
			result.Opportunities = append(result.Opportunities, OpportunitySignal{Message: "感应到机缘", Distance: rules.Distance(scannerPosition, opportunity.Position)})
		}
	}
	sortScan(result.Roles, result.Opportunities)
	if len(result.Roles) > 100 {
		result.Roles = result.Roles[:100]
		result.HasMore = true
	}
	if len(result.Opportunities) > 20 {
		result.Opportunities = result.Opportunities[:20]
		result.HasMore = true
	}
	if err := s.persistLocked(); err != nil {
		return ScanResult{}, err
	}
	return result, nil
}

func (s *Service) Transfer(roleID, targetID, idempotencyKey string, amountMinutes int64) (State, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return State{}, ErrIdempotencyKey
	}
	if amountMinutes <= 0 {
		return State{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.idempotencyResultLocked(roleID, idempotencyKey); ok {
		return previous, nil
	}
	sender, ok := s.roles[roleID]
	if !ok {
		return State{}, ErrUnauthenticated
	}
	receiver, ok := s.roles[targetID]
	if !ok || receiver == sender {
		return State{}, ErrNotFound
	}
	now := s.authoritativeNowLocked(sender)
	senderState := s.stateLocked(sender, now)
	receiverState := s.stateLocked(receiver, now)
	if sender.Status != "alive" || receiver.Status != "alive" {
		return State{}, ErrNotAlive
	}
	if distanceOfStates(senderState, receiverState) > float64(senderState.Speed) {
		return State{}, ErrForbidden
	}
	s.settleOpportunityLocked(sender, now)
	s.settleOpportunityLocked(receiver, now)
	senderCultivation := s.cultivationLocked(sender, now)
	receiverCultivation := s.cultivationLocked(receiver, now)
	amount := rules.Points(float64(amountMinutes))
	senderAge := durationSeconds(senderState.AgeSeconds)
	validation := rules.ValidateTransfer(senderCultivation, amount, senderAge)
	if !validation.Allowed {
		return State{}, ErrForbidden
	}
	sender.CultivationBase = senderCultivation - amount
	sender.CultivationAt = now
	receiver.CultivationBase = receiverCultivation + amount
	receiver.CultivationAt = now
	s.rebaseTrajectoryLocked(sender, positionOfState(senderState), now, sender.CultivationBase)
	s.rebaseTrajectoryLocked(receiver, positionOfState(receiverState), now, receiver.CultivationBase)
	sender.NextDeathAt = now.Add(rules.NextNaturalDeathAfter(sender.CultivationBase, senderAge))
	receiver.NextDeathAt = now.Add(rules.NextNaturalDeathAfter(receiver.CultivationBase, durationSeconds(receiverState.AgeSeconds)))
	sender.StateVersion++
	receiver.StateVersion++
	s.appendEventLocked(sender, now, "transfer", "向"+receiver.Name+"传功", map[string]any{"target_id": receiver.ID, "amount_minutes": amountMinutes})
	s.appendEventLocked(receiver, now, "transfer_received", "收到"+sender.Name+"传功", map[string]any{"source_id": sender.ID, "amount_minutes": amountMinutes})
	if validation.DeathAfterTransfer {
		s.killLocked(sender, now, sender.CultivationBase, sender.Position, "lifespan", true)
	}
	result := s.stateLocked(sender, now)
	s.rememberIdempotencyLocked(roleID, idempotencyKey, result)
	if err := s.persistLocked(); err != nil {
		return State{}, err
	}
	return result, nil
}

func (s *Service) Seize(roleID, targetID, idempotencyKey string) (State, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return State{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.idempotencyResultLocked(roleID, idempotencyKey); ok {
		return previous, nil
	}
	attacker, ok := s.roles[roleID]
	if !ok {
		return State{}, ErrUnauthenticated
	}
	target, ok := s.roles[targetID]
	if !ok || target == attacker {
		return State{}, ErrNotFound
	}
	now := s.authoritativeNowLocked(attacker)
	attackerState := s.stateLocked(attacker, now)
	targetState := s.stateLocked(target, now)
	if attacker.Status != "alive" || target.Status != "alive" {
		return State{}, ErrNotAlive
	}
	if attackerState.RealmLevel <= targetState.RealmLevel || attackerState.Position != targetState.Position {
		return State{}, ErrForbidden
	}
	s.settleOpportunityLocked(attacker, now)
	s.settleOpportunityLocked(target, now)
	taken := s.cultivationLocked(target, now)
	attacker.CultivationBase = s.cultivationLocked(attacker, now) + taken
	attacker.CultivationAt = now
	s.rebaseTrajectoryLocked(attacker, positionOfState(attackerState), now, attacker.CultivationBase)
	attacker.StateVersion++
	target.CultivationBase = 0
	target.CultivationAt = now
	s.killLocked(target, now, 0, positionOfState(targetState), "seizure", false)
	s.appendEventLocked(attacker, now, "seizure", "夺取"+target.Name+"全部修为", map[string]any{"target_id": target.ID, "cultivation": taken.Points()})
	result := s.stateLocked(attacker, now)
	s.rememberIdempotencyLocked(roleID, idempotencyKey, result)
	if err := s.persistLocked(); err != nil {
		return State{}, err
	}
	return result, nil
}

func (s *Service) RequestConversation(roleID, targetID, idempotencyKey string) (Conversation, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Conversation{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	commandKey := roleID + "\x00" + idempotencyKey
	if existingID := s.conversationResults[commandKey]; existingID != "" {
		return *s.conversations[existingID], nil
	}
	requester, ok := s.roles[roleID]
	if !ok {
		return Conversation{}, ErrUnauthenticated
	}
	recipient, ok := s.roles[targetID]
	if !ok || recipient == requester {
		return Conversation{}, ErrNotFound
	}
	now := s.authoritativeNowLocked(requester)
	requesterState := s.stateLocked(requester, now)
	recipientState := s.stateLocked(recipient, now)
	if requester.Status != "alive" || recipient.Status != "alive" {
		return Conversation{}, ErrNotAlive
	}
	if distanceOfStates(requesterState, recipientState) > float64(requesterState.SenseRadius) {
		return Conversation{}, ErrForbidden
	}
	s.nextID++
	id := fmt.Sprintf("conversation_%d", s.nextID)
	conversation := &Conversation{
		ID: id, RequesterID: roleID, RecipientID: targetID, Status: "requested",
		Messages: []ConversationMessage{}, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
	}
	s.conversations[id] = conversation
	s.conversationResults[commandKey] = id
	s.appendEventLocked(requester, now, "conversation_requested", "向"+recipient.Name+"请求交谈", map[string]any{"conversation_id": id})
	s.appendEventLocked(recipient, now, "conversation_incoming", requester.Name+"请求交谈", map[string]any{"conversation_id": id})
	if err := s.persistLocked(); err != nil {
		return Conversation{}, err
	}
	return *conversation, nil
}

func (s *Service) RespondConversation(roleID, conversationID, idempotencyKey, action string) (Conversation, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Conversation{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return Conversation{}, ErrNotFound
	}
	if conversation.RecipientID != roleID {
		return Conversation{}, ErrForbidden
	}
	commandKey := roleID + "\x00" + idempotencyKey
	if s.conversationResults[commandKey] != "" {
		return *conversation, nil
	}
	if conversation.Status != "requested" {
		return Conversation{}, ErrForbidden
	}
	switch action {
	case "accept":
		conversation.Status = "accepted"
	case "reject":
		conversation.Status = "rejected"
	case "ignore":
		conversation.Status = "requested"
	default:
		return Conversation{}, ErrInvalid
	}
	now := s.authoritativeNowLocked(s.roles[roleID])
	conversation.UpdatedAt = now.UnixMilli()
	s.conversationResults[commandKey] = conversationID
	s.appendEventLocked(s.roles[conversation.RequesterID], now, "conversation_responded", "交谈请求状态已更新", map[string]any{"conversation_id": conversationID, "status": conversation.Status})
	if err := s.persistLocked(); err != nil {
		return Conversation{}, err
	}
	return *conversation, nil
}

func (s *Service) SendConversationMessage(roleID, conversationID, idempotencyKey, content string) (ConversationMessage, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return ConversationMessage{}, ErrIdempotencyKey
	}
	content = strings.TrimSpace(content)
	if content == "" || len(content) > 4000 {
		return ConversationMessage{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return ConversationMessage{}, ErrNotFound
	}
	if conversation.Status != "accepted" || (conversation.RequesterID != roleID && conversation.RecipientID != roleID) {
		return ConversationMessage{}, ErrForbidden
	}
	commandKey := roleID + "\x00" + idempotencyKey
	if s.conversationResults[commandKey] != "" {
		for i := len(conversation.Messages) - 1; i >= 0; i-- {
			if conversation.Messages[i].SenderID == roleID {
				return conversation.Messages[i], nil
			}
		}
	}
	now := s.authoritativeNowLocked(s.roles[roleID])
	s.eventSequence++
	message := ConversationMessage{ID: s.eventSequence, SenderID: roleID, Content: content, Trusted: false, CreatedAt: now.UnixMilli()}
	conversation.Messages = append(conversation.Messages, message)
	conversation.UpdatedAt = now.UnixMilli()
	s.conversationResults[commandKey] = conversationID
	otherID := conversation.RequesterID
	if otherID == roleID {
		otherID = conversation.RecipientID
	}
	s.appendEventLocked(s.roles[otherID], now, "conversation_message", "收到新的交谈消息", map[string]any{"conversation_id": conversationID})
	if err := s.persistLocked(); err != nil {
		return ConversationMessage{}, err
	}
	return message, nil
}

func (s *Service) CloseConversation(roleID, conversationID, idempotencyKey string) (Conversation, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Conversation{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return Conversation{}, ErrNotFound
	}
	if conversation.RequesterID != roleID && conversation.RecipientID != roleID {
		return Conversation{}, ErrForbidden
	}
	commandKey := roleID + "\x00" + idempotencyKey
	if s.conversationResults[commandKey] != "" {
		return *conversation, nil
	}
	conversation.Status = "closed"
	now := s.authoritativeNowLocked(s.roles[roleID])
	conversation.UpdatedAt = now.UnixMilli()
	s.conversationResults[commandKey] = conversationID
	if err := s.persistLocked(); err != nil {
		return Conversation{}, err
	}
	return *conversation, nil
}

func (s *Service) Conversations(roleID string) ([]Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.roles[roleID]; !ok {
		return nil, ErrUnauthenticated
	}
	result := make([]Conversation, 0)
	for _, conversation := range s.conversations {
		if conversation.RequesterID == roleID || conversation.RecipientID == roleID {
			copy := *conversation
			copy.Messages = append([]ConversationMessage(nil), conversation.Messages...)
			result = append(result, copy)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt > result[j].UpdatedAt })
	return result, nil
}

func (s *Service) Events(roleID string, after int64, limit int) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.roles[roleID]; !ok {
		return nil, ErrUnauthenticated
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	result := make([]Event, 0, limit)
	for _, event := range s.events[roleID] {
		if event.ID <= after {
			continue
		}
		result = append(result, event)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *Service) Bounds() Bounds {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Bounds{MinX: s.minX.Units(), MaxX: s.maxX.Units(), MinY: s.minY.Units(), MaxY: s.maxY.Units()}
}

func (s *Service) Reincarnate(roleID, idempotencyKey string, position *rules.Position) (State, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return State{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.idempotencyResultLocked(roleID, idempotencyKey); ok {
		return previous, nil
	}
	role, ok := s.roles[roleID]
	if !ok {
		return State{}, ErrUnauthenticated
	}
	now := s.authoritativeNowLocked(role)
	s.stateLocked(role, now)
	if role.Status != "pending_reincarnation" {
		return State{}, ErrForbidden
	}
	chosen := rules.Position{}
	if position != nil {
		chosen = *position
		if chosen.X < s.minX || chosen.X > s.maxX || chosen.Y < s.minY || chosen.Y > s.maxY {
			return State{}, ErrInvalid
		}
	} else {
		var err error
		chosen.X, err = randomCoordinate(s.minX, s.maxX)
		if err != nil {
			return State{}, err
		}
		chosen.Y, err = randomCoordinate(s.minY, s.maxY)
		if err != nil {
			return State{}, err
		}
	}
	role.LifeNumber++
	role.Status = "alive"
	role.LifeStartedAt = now
	role.CultivationAt = now
	role.CultivationBase = 0
	role.LastSettledAt = now
	role.NextDeathAt = now.Add(rules.NextNaturalDeathAfter(0, 0))
	role.Position = chosen
	role.Trajectory = nil
	role.TrajectoryCultivation = 0
	role.StateVersion++
	s.expandBoundsLocked(chosen)
	s.appendEventLocked(role, now, "reincarnation", "完成转世", nil)
	result := s.stateLocked(role, now)
	s.rememberIdempotencyLocked(roleID, idempotencyKey, result)
	if err := s.persistLocked(); err != nil {
		return State{}, err
	}
	return result, nil
}

func (s *Service) authoritativeNowLocked(role *Role) time.Time {
	now := s.clock.Now().UTC()
	if role != nil && now.Before(role.LastSettledAt) {
		return role.LastSettledAt
	}
	return now
}

func (s *Service) cultivationLocked(role *Role, now time.Time) rules.Cultivation {
	if role.Status != "alive" {
		return 0
	}
	elapsed := now.Sub(role.CultivationAt)
	if elapsed < 0 {
		elapsed = 0
	}
	cultivation := role.CultivationBase + rules.Cultivation(elapsed.Milliseconds())
	if opportunity := s.opportunities[role.BoundOpportunityID]; opportunity != nil && opportunity.Status == "bound" {
		converted := rules.ConvertedCultivation(opportunity.Cultivation, now.Sub(opportunity.BoundAt))
		if converted > opportunity.Credited {
			cultivation += converted - opportunity.Credited
		}
	}
	return cultivation
}

func (s *Service) stateLocked(role *Role, now time.Time) State {
	cultivation := s.cultivationLocked(role, now)
	age := now.Sub(role.LifeStartedAt)
	if age < 0 {
		age = 0
	}
	position := role.Position
	movementState := "idle"
	if role.Status == "alive" && role.Trajectory != nil {
		var arrived bool
		travelled := rules.NaturalTravelDistance(role.TrajectoryCultivation, now.Sub(role.Trajectory.StartedAt))
		position, arrived = role.Trajectory.PositionAfterDistance(travelled)
		if arrived {
			role.Position = position
			role.Trajectory = nil
			role.TrajectoryCultivation = 0
			role.StateVersion++
			s.expandBoundsLocked(position)
		} else {
			movementState = "moving"
			s.expandBoundsLocked(position)
		}
	}
	if role.Status == "alive" {
		s.claimOpportunityLocked(role, position, now)
		cultivation = s.cultivationLocked(role, now)
	}
	life := rules.DeriveLife(cultivation, age)
	for role.Status == "alive" && !role.NextDeathAt.IsZero() && !now.Before(role.NextDeathAt) {
		deathAt := role.NextDeathAt
		deathCultivation := s.cultivationLocked(role, deathAt)
		deathAge := deathAt.Sub(role.LifeStartedAt)
		if rules.DeriveLife(deathCultivation, deathAge).Alive {
			role.NextDeathAt = deathAt.Add(rules.NextNaturalDeathAfter(deathCultivation, deathAge))
			continue
		}
		deathPosition := role.Position
		if role.Trajectory != nil {
			travelled := rules.NaturalTravelDistance(role.TrajectoryCultivation, deathAt.Sub(role.Trajectory.StartedAt))
			deathPosition, _ = role.Trajectory.PositionAfterDistance(travelled)
		}
		s.killLocked(role, deathAt, deathCultivation, deathPosition, "lifespan", true)
		cultivation = 0
		age = 0
		life = rules.DeriveLife(0, 0)
		position = deathPosition
		movementState = "idle"
	}
	if role.Status != "alive" {
		cultivation = 0
		age = 0
		life = rules.DeriveLife(0, 0)
	}
	role.LastSettledAt = now
	return State{
		ID:              role.ID,
		Name:            role.Name,
		LifeNumber:      role.LifeNumber,
		Status:          role.Status,
		Cultivation:     cultivation.Points(),
		RealmLevel:      life.Realm.Level,
		Realm:           life.Realm.Name,
		AgeSeconds:      age.Seconds(),
		LifespanSeconds: life.Realm.Lifespan.Seconds(),
		Speed:           life.Realm.Speed,
		SenseRadius:     life.Realm.SenseRadius,
		Position:        PublicPosition{X: position.X.Units(), Y: position.Y.Units()},
		MovementState:   movementState,
		StateVersion:    role.StateVersion,
	}
}

func (s *Service) claimOpportunityLocked(role *Role, position rules.Position, now time.Time) {
	if role.BoundOpportunityID != "" {
		return
	}
	ids := make([]string, 0, len(s.opportunities))
	for id := range s.opportunities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		opportunity := s.opportunities[id]
		if opportunity.Status != "unclaimed" || opportunity.Position != position {
			continue
		}
		opportunity.Status = "bound"
		opportunity.BoundRoleID = role.ID
		opportunity.BoundAt = now
		role.BoundOpportunityID = id
		role.StateVersion++
		s.appendEventLocked(role, now, "opportunity_claimed", "觅得机缘", map[string]any{"opportunity_id": id})
		return
	}
}

func (s *Service) settleOpportunityLocked(role *Role, now time.Time) {
	opportunity := s.opportunities[role.BoundOpportunityID]
	if opportunity == nil || opportunity.Status != "bound" {
		return
	}
	converted := rules.ConvertedCultivation(opportunity.Cultivation, now.Sub(opportunity.BoundAt))
	if converted > opportunity.Credited {
		role.CultivationBase += converted - opportunity.Credited
		opportunity.Credited = converted
	}
	if converted == opportunity.Cultivation {
		opportunity.Status = "consumed"
		role.BoundOpportunityID = ""
		s.appendEventLocked(role, now, "opportunity_converted", "参悟机缘", map[string]any{"cultivation": converted.Points()})
	}
}

func (s *Service) killLocked(role *Role, at time.Time, cultivation rules.Cultivation, position rules.Position, cause string, createOpportunity bool) {
	if role.Status != "alive" {
		return
	}
	role.Status = "pending_reincarnation"
	role.CultivationBase = 0
	role.CultivationAt = at
	role.Position = position
	role.Trajectory = nil
	role.TrajectoryCultivation = 0
	role.NextDeathAt = time.Time{}
	role.StateVersion++
	s.expandBoundsLocked(position)
	if opportunity := s.opportunities[role.BoundOpportunityID]; opportunity != nil {
		opportunity.Status = "discarded"
		opportunity.BoundRoleID = ""
		role.BoundOpportunityID = ""
	}
	data := map[string]any{"cause": cause}
	if createOpportunity && cultivation > 0 {
		s.nextID++
		opportunityID := fmt.Sprintf("opportunity_%d", s.nextID)
		s.opportunities[opportunityID] = &Opportunity{
			ID: opportunityID, Position: rules.Position{X: s.minX, Y: s.minY},
			Cultivation: cultivation, SenseRadius: rules.Units(1), Status: "unclaimed",
		}
		data["opportunity_created"] = true
	}
	s.appendEventLocked(role, at, "death", "本世身死，等待转世", data)
}

func (s *Service) appendEventLocked(role *Role, at time.Time, eventType, message string, data map[string]any) {
	s.eventSequence++
	s.events[role.ID] = append(s.events[role.ID], Event{
		ID: s.eventSequence, Type: eventType, Message: message, CreatedAt: at.UnixMilli(), LifeNumber: role.LifeNumber, Data: data,
	})
}

func (s *Service) expandBoundsLocked(position rules.Position) {
	if position.X < s.minX {
		s.minX = position.X
	}
	if position.X > s.maxX {
		s.maxX = position.X
	}
	if position.Y < s.minY {
		s.minY = position.Y
	}
	if position.Y > s.maxY {
		s.maxY = position.Y
	}
}

func positionOfState(state State) rules.Position {
	return rules.Position{X: rules.Units(state.Position.X), Y: rules.Units(state.Position.Y)}
}

func distanceOfStates(a, b State) float64 {
	return rules.Distance(positionOfState(a), positionOfState(b))
}

func (s *Service) rebaseTrajectoryLocked(role *Role, position rules.Position, now time.Time, cultivation rules.Cultivation) {
	if role.Trajectory == nil {
		return
	}
	target := role.Trajectory.Target
	role.Position = position
	role.Trajectory = &rules.Trajectory{Start: position, Target: target, StartedAt: now, Speed: rules.RealmFor(cultivation).Speed}
	role.TrajectoryCultivation = cultivation
}

func durationSeconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}

func direction(from, to rules.Position) string {
	dx, dy := to.X-from.X, to.Y-from.Y
	switch {
	case dx == 0 && dy == 0:
		return "同一位置"
	case absCoordinate(dx) >= absCoordinate(dy) && dx > 0:
		return "东"
	case absCoordinate(dx) >= absCoordinate(dy):
		return "西"
	case dy > 0:
		return "北"
	default:
		return "南"
	}
}

func absCoordinate(value rules.Coordinate) rules.Coordinate {
	if value < 0 {
		return -value
	}
	return value
}

func sortScan(roles []ScanRole, opportunities []OpportunitySignal) {
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].Distance == roles[j].Distance {
			return roles[i].ID < roles[j].ID
		}
		return roles[i].Distance < roles[j].Distance
	})
	sort.Slice(opportunities, func(i, j int) bool {
		return opportunities[i].Distance < opportunities[j].Distance
	})
}

type snapshotAccount struct {
	PasswordHash []byte `json:"password_hash"`
	RoleID       string `json:"role_id"`
}

type snapshotSession struct {
	RoleID    string    `json:"role_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type serviceSnapshot struct {
	Accounts            map[string]snapshotAccount  `json:"accounts"`
	RoleNames           map[string]string           `json:"role_names"`
	Roles               map[string]*Role            `json:"roles"`
	Sessions            map[string]snapshotSession  `json:"sessions"`
	Idempotency         map[string]map[string]State `json:"idempotency"`
	Events              map[string][]Event          `json:"events"`
	Opportunities       map[string]*Opportunity     `json:"opportunities"`
	Conversations       map[string]*Conversation    `json:"conversations"`
	ConversationResults map[string]string           `json:"conversation_results"`
	EventSequence       int64                       `json:"event_sequence"`
	MinX                rules.Coordinate            `json:"min_x"`
	MaxX                rules.Coordinate            `json:"max_x"`
	MinY                rules.Coordinate            `json:"min_y"`
	MaxY                rules.Coordinate            `json:"max_y"`
	NextID              uint64                      `json:"next_id"`
}

func (s *Service) persistLocked() error {
	if s.store == nil {
		return nil
	}
	accounts := make(map[string]snapshotAccount, len(s.accounts))
	for name, value := range s.accounts {
		accounts[name] = snapshotAccount{PasswordHash: value.passwordHash, RoleID: value.roleID}
	}
	sessions := make(map[string]snapshotSession, len(s.sessions))
	for hash, value := range s.sessions {
		sessions[hex.EncodeToString(hash[:])] = snapshotSession{RoleID: value.RoleID, ExpiresAt: value.ExpiresAt}
	}
	payload, err := json.Marshal(serviceSnapshot{
		Accounts: accounts, RoleNames: s.roleNames, Roles: s.roles, Sessions: sessions,
		Idempotency: s.idempotency, Events: s.events, Opportunities: s.opportunities,
		Conversations: s.conversations, ConversationResults: s.conversationResults,
		EventSequence: s.eventSequence, MinX: s.minX, MaxX: s.maxX, MinY: s.minY, MaxY: s.maxY, NextID: s.nextID,
	})
	if err != nil {
		return fmt.Errorf("encode world snapshot: %w", err)
	}
	if err := s.store.Save(context.Background(), payload); err != nil {
		return fmt.Errorf("persist world snapshot: %w", err)
	}
	return nil
}

func (s *Service) restoreLocked(payload []byte) error {
	var snapshot serviceSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return err
	}
	s.accounts = make(map[string]account, len(snapshot.Accounts))
	for name, value := range snapshot.Accounts {
		s.accounts[name] = account{passwordHash: value.PasswordHash, roleID: value.RoleID}
	}
	s.sessions = make(map[[32]byte]session, len(snapshot.Sessions))
	for encoded, value := range snapshot.Sessions {
		decoded, err := hex.DecodeString(encoded)
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("invalid stored session hash")
		}
		var hash [32]byte
		copy(hash[:], decoded)
		s.sessions[hash] = session{RoleID: value.RoleID, ExpiresAt: value.ExpiresAt}
	}
	s.roleNames = snapshot.RoleNames
	s.roles = snapshot.Roles
	s.idempotency = snapshot.Idempotency
	s.events = snapshot.Events
	s.opportunities = snapshot.Opportunities
	s.conversations = snapshot.Conversations
	s.conversationResults = snapshot.ConversationResults
	s.eventSequence = snapshot.EventSequence
	s.minX, s.maxX, s.minY, s.maxY = snapshot.MinX, snapshot.MaxX, snapshot.MinY, snapshot.MaxY
	s.nextID = snapshot.NextID
	if s.roleNames == nil {
		s.roleNames = make(map[string]string)
	}
	if s.roles == nil {
		s.roles = make(map[string]*Role)
	}
	if s.idempotency == nil {
		s.idempotency = make(map[string]map[string]State)
	}
	if s.events == nil {
		s.events = make(map[string][]Event)
	}
	if s.opportunities == nil {
		s.opportunities = make(map[string]*Opportunity)
	}
	if s.conversations == nil {
		s.conversations = make(map[string]*Conversation)
	}
	if s.conversationResults == nil {
		s.conversationResults = make(map[string]string)
	}
	return nil
}

func (s *Service) idempotencyResultLocked(roleID, key string) (State, bool) {
	results := s.idempotency[roleID]
	if results == nil {
		return State{}, false
	}
	result, ok := results[key]
	return result, ok
}

func (s *Service) rememberIdempotencyLocked(roleID, key string, result State) {
	if s.idempotency[roleID] == nil {
		s.idempotency[roleID] = make(map[string]State)
	}
	s.idempotency[roleID][key] = result
}

func newToken() (string, [32]byte, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	return token, sha256.Sum256([]byte(token)), nil
}

func randomCoordinate(minimum, maximum rules.Coordinate) (rules.Coordinate, error) {
	span := int64(maximum-minimum) + 1
	if span <= 1 {
		return minimum, nil
	}
	value, err := rand.Int(rand.Reader, big.NewInt(span))
	if err != nil {
		return 0, fmt.Errorf("generate random coordinate: %w", err)
	}
	return minimum + rules.Coordinate(value.Int64()), nil
}
