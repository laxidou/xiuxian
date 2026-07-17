package world

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"xiuxian/internal/rules"
)

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
		if committed, loadErr := s.store.Load(context.Background()); loadErr == nil && len(committed) > 0 {
			_ = s.restoreLocked(committed)
		} else if loadErr == nil {
			s.resetStateLocked()
		}
		return fmt.Errorf("persist world snapshot: %w", err)
	}
	return nil
}

func (s *Service) resetStateLocked() {
	s.accounts = make(map[string]account)
	s.roleNames = make(map[string]string)
	s.roles = make(map[string]*Role)
	s.sessions = make(map[[32]byte]session)
	s.idempotency = make(map[string]map[string]State)
	s.events = make(map[string][]Event)
	s.opportunities = make(map[string]*Opportunity)
	s.conversations = make(map[string]*Conversation)
	s.conversationResults = make(map[string]string)
	s.eventSequence = 0
	s.minX, s.maxX, s.minY, s.maxY = 0, 0, 0, 0
	s.nextID = 0
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
