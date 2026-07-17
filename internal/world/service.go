package world

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	ErrStaleCommand    = errors.New("command expectation is stale")
)

type CommandExpectation struct {
	LifeNumber   int64
	StateVersion int64
}

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
	Status                RoleStatus
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
	Status          RoleStatus     `json:"status"`
	Cultivation     float64        `json:"cultivation"`
	RealmLevel      int            `json:"realm_level"`
	Realm           string         `json:"realm"`
	AgeSeconds      float64        `json:"age_seconds"`
	LifespanSeconds float64        `json:"lifespan_seconds"`
	Speed           int64          `json:"speed"`
	SenseRadius     int64          `json:"sense_radius"`
	Position        PublicPosition `json:"position"`
	MovementState   MovementState  `json:"movement_state"`
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

type RoleStatus string

const (
	RoleAlive                RoleStatus = "alive"
	RolePendingReincarnation RoleStatus = "pending_reincarnation"
)

type OpportunityStatus string

const (
	OpportunityUnplaced  OpportunityStatus = "unplaced"
	OpportunityUnclaimed OpportunityStatus = "unclaimed"
	OpportunityBound     OpportunityStatus = "bound"
	OpportunityConsumed  OpportunityStatus = "consumed"
	OpportunityDiscarded OpportunityStatus = "discarded"
)

type ConversationStatus string

const (
	ConversationRequested ConversationStatus = "requested"
	ConversationAccepted  ConversationStatus = "accepted"
	ConversationRejected  ConversationStatus = "rejected"
	ConversationClosed    ConversationStatus = "closed"
)

type MovementState string

const (
	MovementIdle   MovementState = "idle"
	MovementMoving MovementState = "moving"
)

type EventType string

type session struct {
	RoleID    string
	ExpiresAt time.Time
}

type Event struct {
	ID         int64          `json:"id"`
	Type       EventType      `json:"type"`
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
	ID            string
	Position      rules.Position
	Level         int
	Cultivation   rules.Cultivation
	SenseRadius   rules.Coordinate
	Status        OpportunityStatus
	BoundRoleID   string
	BoundAt       time.Time
	Credited      rules.Cultivation
	DeathPosition rules.Position
}

type ScanRole struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Realm    string          `json:"realm"`
	Status   RoleStatus      `json:"status"`
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
	Status      ConversationStatus    `json:"status"`
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

type authorityTimeStore interface {
	AuthorityNow(context.Context) (time.Time, error)
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
		Status:        RoleAlive,
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

func (s *Service) SettleDeadline(roleID string, expectedVersion int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.roles[roleID]
	if !ok {
		return false, ErrNotFound
	}
	if role.StateVersion != expectedVersion || role.Status != RoleAlive {
		return false, nil
	}
	before := role.Status
	now := s.authoritativeNowLocked(role)
	s.stateLocked(role, now)
	if err := s.persistLocked(); err != nil {
		return false, err
	}
	return before == RoleAlive && role.Status == RolePendingReincarnation, nil
}

func (s *Service) Move(roleID, idempotencyKey string, target rules.Position, expectation CommandExpectation) (State, error) {
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
	if err := validateCommandExpectation(role, expectation); err != nil {
		return State{}, err
	}
	now := s.authoritativeNowLocked(role)
	current := s.stateLocked(role, now)
	if role.Status != RoleAlive {
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

func (s *Service) Stop(roleID, idempotencyKey string, expectation CommandExpectation) (State, error) {
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
	if err := validateCommandExpectation(role, expectation); err != nil {
		return State{}, err
	}
	now := s.authoritativeNowLocked(role)
	current := s.stateLocked(role, now)
	if role.Status != RoleAlive {
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

func (s *Service) Scan(roleID string, expectation CommandExpectation) (ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scanner, ok := s.roles[roleID]
	if !ok {
		return ScanResult{}, ErrUnauthenticated
	}
	if err := validateCommandExpectation(scanner, expectation); err != nil {
		return ScanResult{}, err
	}
	now := s.authoritativeNowLocked(scanner)
	scannerState := s.stateLocked(scanner, now)
	if scanner.Status != RoleAlive {
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
		if target.Status != RoleAlive {
			continue
		}
		targetPosition := positionOfState(targetState)
		distance := rules.Distance(scannerPosition, targetPosition)
		if distance > float64(scannerState.SenseRadius) {
			continue
		}
		entry := ScanRole{ID: target.ID, Name: target.Name, Realm: targetState.Realm, Status: targetState.Status, Distance: distance}
		position := targetState.Position
		entry.Position = &position
		if scannerState.RealmLevel > targetState.RealmLevel {
			s.appendEventLocked(target, now, "scanned", "被更高境界角色神识扫描", map[string]any{
				"direction":    direction(targetPosition, scannerPosition),
				"scanner_name": scanner.Name,
			})
		}
		result.Roles = append(result.Roles, entry)
	}
	for _, opportunity := range s.opportunities {
		if opportunity.Status != OpportunityUnclaimed {
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

func (s *Service) Transfer(roleID, targetID, idempotencyKey string, amountMinutes int64, expectation CommandExpectation) (State, error) {
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
	if err := validateCommandExpectation(sender, expectation); err != nil {
		return State{}, err
	}
	receiver, ok := s.roles[targetID]
	if !ok || receiver == sender {
		return State{}, ErrNotFound
	}
	now := s.authoritativeNowLocked(sender)
	senderState := s.stateLocked(sender, now)
	receiverState := s.stateLocked(receiver, now)
	if sender.Status != RoleAlive || receiver.Status != RoleAlive {
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

func (s *Service) Seize(roleID, targetID, idempotencyKey string, expectation CommandExpectation) (State, error) {
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
	if err := validateCommandExpectation(attacker, expectation); err != nil {
		return State{}, err
	}
	target, ok := s.roles[targetID]
	if !ok || target == attacker {
		return State{}, ErrNotFound
	}
	now := s.authoritativeNowLocked(attacker)
	attackerState := s.stateLocked(attacker, now)
	targetState := s.stateLocked(target, now)
	if attacker.Status != RoleAlive || target.Status != RoleAlive {
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

func (s *Service) RequestConversation(roleID, targetID, idempotencyKey string, expectation CommandExpectation) (Conversation, error) {
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
	if err := validateCommandExpectation(requester, expectation); err != nil {
		return Conversation{}, err
	}
	recipient, ok := s.roles[targetID]
	if !ok || recipient == requester {
		return Conversation{}, ErrNotFound
	}
	now := s.authoritativeNowLocked(requester)
	requesterState := s.stateLocked(requester, now)
	recipientState := s.stateLocked(recipient, now)
	if requester.Status != RoleAlive || recipient.Status != RoleAlive {
		return Conversation{}, ErrNotAlive
	}
	if distanceOfStates(requesterState, recipientState) > float64(requesterState.SenseRadius) {
		return Conversation{}, ErrForbidden
	}
	s.nextID++
	id := fmt.Sprintf("conversation_%d", s.nextID)
	conversation := &Conversation{
		ID: id, RequesterID: roleID, RecipientID: targetID, Status: ConversationRequested,
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

func (s *Service) RespondConversation(roleID, conversationID, idempotencyKey, action string, expectation CommandExpectation) (Conversation, error) {
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
	if err := validateCommandExpectation(s.roles[roleID], expectation); err != nil {
		return Conversation{}, err
	}
	now := s.authoritativeNowLocked(s.roles[roleID])
	s.stateLocked(s.roles[conversation.RequesterID], now)
	s.stateLocked(s.roles[conversation.RecipientID], now)
	if s.roles[conversation.RequesterID].Status != RoleAlive || s.roles[conversation.RecipientID].Status != RoleAlive {
		return Conversation{}, ErrNotAlive
	}
	commandKey := roleID + "\x00" + idempotencyKey
	if s.conversationResults[commandKey] != "" {
		return *conversation, nil
	}
	if conversation.Status != ConversationRequested {
		return Conversation{}, ErrForbidden
	}
	switch action {
	case "accept":
		conversation.Status = ConversationAccepted
	case "reject":
		conversation.Status = ConversationRejected
	case "ignore":
		conversation.Status = ConversationRequested
	default:
		return Conversation{}, ErrInvalid
	}
	conversation.UpdatedAt = now.UnixMilli()
	s.conversationResults[commandKey] = conversationID
	s.appendEventLocked(s.roles[conversation.RequesterID], now, "conversation_responded", "交谈请求状态已更新", map[string]any{"conversation_id": conversationID, "status": conversation.Status})
	if err := s.persistLocked(); err != nil {
		return Conversation{}, err
	}
	return *conversation, nil
}

func (s *Service) SendConversationMessage(roleID, conversationID, idempotencyKey, content string, expectation CommandExpectation) (ConversationMessage, error) {
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
	if conversation.Status != ConversationAccepted || (conversation.RequesterID != roleID && conversation.RecipientID != roleID) {
		return ConversationMessage{}, ErrForbidden
	}
	if err := validateCommandExpectation(s.roles[roleID], expectation); err != nil {
		return ConversationMessage{}, err
	}
	now := s.authoritativeNowLocked(s.roles[roleID])
	s.stateLocked(s.roles[conversation.RequesterID], now)
	s.stateLocked(s.roles[conversation.RecipientID], now)
	if s.roles[conversation.RequesterID].Status != RoleAlive || s.roles[conversation.RecipientID].Status != RoleAlive {
		return ConversationMessage{}, ErrNotAlive
	}
	commandKey := roleID + "\x00" + idempotencyKey
	if s.conversationResults[commandKey] != "" {
		for i := len(conversation.Messages) - 1; i >= 0; i-- {
			if conversation.Messages[i].SenderID == roleID {
				return conversation.Messages[i], nil
			}
		}
	}
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

func (s *Service) CloseConversation(roleID, conversationID, idempotencyKey string, expectation CommandExpectation) (Conversation, error) {
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
	if err := validateCommandExpectation(s.roles[roleID], expectation); err != nil {
		return Conversation{}, err
	}
	now := s.authoritativeNowLocked(s.roles[roleID])
	s.stateLocked(s.roles[conversation.RequesterID], now)
	s.stateLocked(s.roles[conversation.RecipientID], now)
	if s.roles[conversation.RequesterID].Status != RoleAlive || s.roles[conversation.RecipientID].Status != RoleAlive {
		return Conversation{}, ErrNotAlive
	}
	commandKey := roleID + "\x00" + idempotencyKey
	if s.conversationResults[commandKey] != "" {
		return *conversation, nil
	}
	conversation.Status = ConversationClosed
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

func (s *Service) Reincarnate(roleID, idempotencyKey string, position *rules.Position, expectation CommandExpectation) (State, error) {
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
	if err := validateCommandExpectation(role, expectation); err != nil {
		return State{}, err
	}
	now := s.authoritativeNowLocked(role)
	s.stateLocked(role, now)
	if role.Status != RolePendingReincarnation {
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
	role.Status = RoleAlive
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
	if databaseClock, ok := s.store.(authorityTimeStore); ok {
		if databaseNow, err := databaseClock.AuthorityNow(context.Background()); err == nil {
			now = databaseNow.UTC()
		} else if role != nil {
			now = role.LastSettledAt
		} else {
			now = time.UnixMilli(0).UTC()
		}
	}
	if role != nil && now.Before(role.LastSettledAt) {
		return role.LastSettledAt
	}
	return now
}

func (s *Service) cultivationLocked(role *Role, now time.Time) rules.Cultivation {
	if role.Status != RoleAlive {
		return 0
	}
	elapsed := now.Sub(role.CultivationAt)
	if elapsed < 0 {
		elapsed = 0
	}
	cultivation := role.CultivationBase + rules.Cultivation(elapsed.Milliseconds())
	if opportunity := s.opportunities[role.BoundOpportunityID]; opportunity != nil && opportunity.Status == OpportunityBound {
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
	movementState := MovementIdle
	if role.Status == RoleAlive && role.Trajectory != nil {
		var arrived bool
		travelled := rules.NaturalTravelDistance(role.TrajectoryCultivation, now.Sub(role.Trajectory.StartedAt))
		position, arrived = role.Trajectory.PositionAfterDistance(travelled)
		if arrived {
			role.Position = position
			role.Trajectory = nil
			role.TrajectoryCultivation = 0
			role.StateVersion++
			s.expandBoundsLocked(position)
			s.appendEventLocked(role, now, "movement_arrived", "已抵达目标位置", map[string]any{"x": position.X.Units(), "y": position.Y.Units()})
		} else {
			movementState = MovementMoving
			s.expandBoundsLocked(position)
		}
	}
	life := rules.DeriveLife(cultivation, age)
	for role.Status == RoleAlive && !role.NextDeathAt.IsZero() && !now.Before(role.NextDeathAt) {
		deathAt := role.NextDeathAt
		s.settleOpportunityLocked(role, deathAt)
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
		movementState = MovementIdle
	}
	if role.Status == RoleAlive {
		s.claimOpportunityLocked(role, position, now)
		trajectoryRealm := rules.Realm{}
		if role.Trajectory != nil {
			trajectoryRealm = rules.RealmFor(role.TrajectoryCultivation)
		}
		s.settleOpportunityLocked(role, now)
		cultivation = s.cultivationLocked(role, now)
		if role.Trajectory != nil && rules.RealmFor(cultivation).Level != trajectoryRealm.Level {
			s.rebaseTrajectoryLocked(role, position, now, cultivation)
			role.StateVersion++
		}
		life = rules.DeriveLife(cultivation, age)
	}
	if role.Status != RoleAlive {
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
		if opportunity.Status != OpportunityUnclaimed || opportunity.Position != position {
			continue
		}
		opportunity.Status = OpportunityBound
		opportunity.BoundRoleID = role.ID
		opportunity.BoundAt = now
		role.BoundOpportunityID = id
		role.StateVersion++
		s.appendEventLocked(role, now, "opportunity_claimed", "觅得机缘", map[string]any{"opportunity_id": id})
		s.appendEventLocked(role, now, "opportunity_converting", "参悟机缘", map[string]any{"opportunity_id": id})
		return
	}
}

func (s *Service) settleOpportunityLocked(role *Role, now time.Time) {
	opportunity := s.opportunities[role.BoundOpportunityID]
	if opportunity == nil || opportunity.Status != OpportunityBound {
		return
	}
	converted := rules.ConvertedCultivation(opportunity.Cultivation, now.Sub(opportunity.BoundAt))
	if converted > opportunity.Credited {
		role.CultivationBase += converted - opportunity.Credited
		opportunity.Credited = converted
	}
	if converted == opportunity.Cultivation {
		opportunity.Status = OpportunityConsumed
		role.BoundOpportunityID = ""
		s.appendEventLocked(role, now, "opportunity_converted", "参悟机缘", map[string]any{"cultivation": converted.Points()})
	}
}

func (s *Service) killLocked(role *Role, at time.Time, cultivation rules.Cultivation, position rules.Position, cause string, createOpportunity bool) {
	if role.Status != RoleAlive {
		return
	}
	role.Status = RolePendingReincarnation
	role.CultivationBase = 0
	role.CultivationAt = at
	role.Position = position
	role.Trajectory = nil
	role.TrajectoryCultivation = 0
	role.NextDeathAt = time.Time{}
	role.StateVersion++
	s.expandBoundsLocked(position)
	s.closeConversationsForDeathLocked(role, at)
	if opportunity := s.opportunities[role.BoundOpportunityID]; opportunity != nil {
		opportunity.Status = OpportunityDiscarded
		opportunity.BoundRoleID = ""
		role.BoundOpportunityID = ""
	}
	data := map[string]any{"cause": cause}
	if createOpportunity && cultivation > 0 {
		s.nextID++
		opportunityID := fmt.Sprintf("opportunity_%d", s.nextID)
		level := rules.RealmFor(cultivation).Level
		radius := rules.RealmFor(cultivation).SenseRadius
		opportunity := &Opportunity{
			ID: opportunityID, Position: position, DeathPosition: position, Level: level,
			Cultivation: cultivation, SenseRadius: rules.Units(float64(radius)), Status: OpportunityUnplaced,
		}
		s.opportunities[opportunityID] = opportunity
		s.placeOpportunityLocked(opportunity)
		data["opportunity_created"] = true
	}
	s.appendEventLocked(role, at, "death", "本世身死，等待转世", data)
}

func (s *Service) appendEventLocked(role *Role, at time.Time, eventType EventType, message string, data map[string]any) {
	s.eventSequence++
	s.events[role.ID] = append(s.events[role.ID], Event{
		ID: s.eventSequence, Type: eventType, Message: message, CreatedAt: at.UnixMilli(), LifeNumber: role.LifeNumber, Data: data,
	})
}

func (s *Service) expandBoundsLocked(position rules.Position) {
	changed := false
	if position.X < s.minX {
		s.minX = position.X
		changed = true
	}
	if position.X > s.maxX {
		s.maxX = position.X
		changed = true
	}
	if position.Y < s.minY {
		s.minY = position.Y
		changed = true
	}
	if position.Y > s.maxY {
		s.maxY = position.Y
		changed = true
	}
	if changed {
		s.placeUnplacedOpportunitiesLocked()
	}
}

func (s *Service) closeConversationsForDeathLocked(role *Role, at time.Time) {
	for _, conversation := range s.conversations {
		if conversation.Status == ConversationClosed || conversation.Status == ConversationRejected {
			continue
		}
		if conversation.RequesterID != role.ID && conversation.RecipientID != role.ID {
			continue
		}
		conversation.Status = ConversationClosed
		conversation.UpdatedAt = at.UnixMilli()
		otherID := conversation.RequesterID
		if otherID == role.ID {
			otherID = conversation.RecipientID
		}
		if other := s.roles[otherID]; other != nil {
			s.appendEventLocked(other, at, "conversation_closed", "交谈因对方本世结束而关闭", map[string]any{"conversation_id": conversation.ID})
		}
	}
}

func (s *Service) placeUnplacedOpportunitiesLocked() {
	ids := make([]string, 0, len(s.opportunities))
	for id, opportunity := range s.opportunities {
		if opportunity.Status == OpportunityUnplaced {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		s.placeOpportunityLocked(s.opportunities[id])
	}
}

func (s *Service) placeOpportunityLocked(opportunity *Opportunity) {
	if opportunity == nil || opportunity.Status != OpportunityUnplaced {
		return
	}
	position, ok := randomPositionExcluding(s.minX, s.maxX, s.minY, s.maxY, opportunity.DeathPosition)
	if !ok {
		return
	}
	opportunity.Position = position
	opportunity.Status = OpportunityUnclaimed
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

func validateCommandExpectation(role *Role, expectation CommandExpectation) error {
	if role == nil {
		return ErrUnauthenticated
	}
	if expectation.LifeNumber <= 0 || expectation.StateVersion <= 0 {
		return ErrInvalid
	}
	if role.LifeNumber != expectation.LifeNumber || role.StateVersion != expectation.StateVersion {
		return ErrStaleCommand
	}
	return nil
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

func randomPositionExcluding(minX, maxX, minY, maxY rules.Coordinate, excluded rules.Position) (rules.Position, bool) {
	if minX == maxX && minY == maxY {
		return rules.Position{}, false
	}
	for range 16 {
		x, errX := randomCoordinate(minX, maxX)
		y, errY := randomCoordinate(minY, maxY)
		if errX != nil || errY != nil {
			break
		}
		candidate := rules.Position{X: x, Y: y}
		if candidate != excluded {
			return candidate, true
		}
	}
	for _, candidate := range []rules.Position{{X: minX, Y: minY}, {X: minX, Y: maxY}, {X: maxX, Y: minY}, {X: maxX, Y: maxY}} {
		if candidate != excluded {
			return candidate, true
		}
	}
	return rules.Position{}, false
}
