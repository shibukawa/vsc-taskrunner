package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"vsc-taskrunner/internal/uiconfig"
)

const (
	AuthMethodSession = "session"
	AuthMethodToken   = "token"

	APITokenScopeRunsRead  = "runs:read"
	APITokenScopeRunsWrite = "runs:write"
)

var (
	ErrAPITokenNotFound    = errors.New("api token not found")
	ErrAPITokenExpired     = errors.New("api token expired")
	ErrAPITokenRevoked     = errors.New("api token revoked")
	ErrAPITokenScopeDenied = errors.New("api token scope denied")
)

type APITokenRecord struct {
	ID         string                 `json:"id"`
	Label      string                 `json:"label"`
	TokenHash  string                 `json:"tokenHash"`
	Subject    string                 `json:"subject"`
	Claims     map[string]interface{} `json:"claims"`
	Scopes     []string               `json:"scopes"`
	CreatedAt  time.Time              `json:"createdAt"`
	LastUsedAt *time.Time             `json:"lastUsedAt,omitempty"`
	ExpiresAt  time.Time              `json:"expiresAt"`
	RevokedAt  *time.Time             `json:"revokedAt,omitempty"`
}

type APITokenView struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

type APITokenStore interface {
	ReadAll(ctx context.Context) ([]*APITokenRecord, error)
	UpdateAll(ctx context.Context, fn func([]*APITokenRecord) ([]*APITokenRecord, error)) error
}

type APITokenService struct {
	config uiconfig.APITokenConfig
	store  APITokenStore
	now    func() time.Time
}

func NewAPITokenService(cfg uiconfig.APITokenConfig, store APITokenStore) *APITokenService {
	return &APITokenService{
		config: cfg,
		store:  store,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *APITokenService) Enabled() bool {
	return s != nil && s.config.Enabled && s.store != nil
}

func (s *APITokenService) List(ctx context.Context, subject string) ([]APITokenView, error) {
	if !s.Enabled() {
		return nil, nil
	}
	records, err := s.store.ReadAll(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]APITokenView, 0, len(records))
	for _, record := range records {
		if record == nil || record.Subject != subject {
			continue
		}
		items = append(items, tokenViewFromRecord(record))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (s *APITokenService) Create(ctx context.Context, subject string, claims map[string]interface{}, label string, scopes []string, ttlHours int) (string, APITokenView, error) {
	if !s.Enabled() {
		return "", APITokenView{}, fmt.Errorf("api tokens are disabled")
	}
	subject = strings.TrimSpace(subject)
	label = strings.TrimSpace(label)
	if subject == "" {
		return "", APITokenView{}, fmt.Errorf("token subject is required")
	}
	if label == "" {
		return "", APITokenView{}, fmt.Errorf("token label is required")
	}
	normalizedScopes, err := normalizeTokenScopes(scopes)
	if err != nil {
		return "", APITokenView{}, err
	}
	if ttlHours <= 0 {
		ttlHours = s.config.DefaultTTLHours
	}
	now := s.now()
	expiresAt := now.Add(time.Duration(ttlHours) * time.Hour)
	tokenValue, err := generateAPITokenValue()
	if err != nil {
		return "", APITokenView{}, err
	}
	record := &APITokenRecord{
		ID:        tokenIDFromValue(tokenValue),
		Label:     label,
		TokenHash: hashAPIToken(tokenValue),
		Subject:   subject,
		Claims:    cloneClaimsMap(claims),
		Scopes:    normalizedScopes,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}
	if err := s.store.UpdateAll(ctx, func(records []*APITokenRecord) ([]*APITokenRecord, error) {
		activeCount := 0
		for _, existing := range records {
			if existing == nil || existing.Subject != subject {
				continue
			}
			if existing.RevokedAt == nil && existing.ExpiresAt.After(now) {
				activeCount++
			}
		}
		if activeCount >= s.config.MaxPerUser {
			return nil, fmt.Errorf("active api token limit reached")
		}
		return append(records, record), nil
	}); err != nil {
		return "", APITokenView{}, err
	}
	return tokenValue, tokenViewFromRecord(record), nil
}

func (s *APITokenService) Revoke(ctx context.Context, subject string, tokenID string) error {
	if !s.Enabled() {
		return fmt.Errorf("api tokens are disabled")
	}
	now := s.now()
	revoked := false
	if err := s.store.UpdateAll(ctx, func(records []*APITokenRecord) ([]*APITokenRecord, error) {
		for _, record := range records {
			if record == nil || record.ID != tokenID || record.Subject != subject {
				continue
			}
			if record.RevokedAt == nil {
				ts := now
				record.RevokedAt = &ts
			}
			revoked = true
			break
		}
		if !revoked {
			return nil, ErrAPITokenNotFound
		}
		return records, nil
	}); err != nil {
		return err
	}
	return nil
}

func (s *APITokenService) Authenticate(ctx context.Context, tokenValue string) (*APITokenRecord, error) {
	if !s.Enabled() {
		return nil, ErrAPITokenNotFound
	}
	tokenHash := hashAPIToken(tokenValue)
	now := s.now()
	var matched *APITokenRecord
	if err := s.store.UpdateAll(ctx, func(records []*APITokenRecord) ([]*APITokenRecord, error) {
		for _, record := range records {
			if record == nil || record.TokenHash != tokenHash {
				continue
			}
			matched = cloneAPITokenRecord(record)
			if record.RevokedAt != nil {
				return records, nil
			}
			if !record.ExpiresAt.After(now) {
				return records, nil
			}
			lastUsedAt := now
			record.LastUsedAt = &lastUsedAt
			matched.LastUsedAt = &lastUsedAt
			return records, nil
		}
		return records, nil
	}); err != nil {
		return nil, err
	}
	if matched == nil {
		return nil, ErrAPITokenNotFound
	}
	if matched.RevokedAt != nil {
		return nil, ErrAPITokenRevoked
	}
	if !matched.ExpiresAt.After(now) {
		return nil, ErrAPITokenExpired
	}
	return matched, nil
}

func (s *APITokenService) HasScopes(record *APITokenRecord, required ...string) bool {
	if len(required) == 0 {
		return true
	}
	if record == nil {
		return false
	}
	scopeSet := make(map[string]struct{}, len(record.Scopes))
	for _, scope := range record.Scopes {
		scopeSet[scope] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := scopeSet[scope]; !ok {
			return false
		}
	}
	return true
}

func tokenViewFromRecord(record *APITokenRecord) APITokenView {
	return APITokenView{
		ID:         record.ID,
		Label:      record.Label,
		Scopes:     append([]string(nil), record.Scopes...),
		CreatedAt:  record.CreatedAt,
		LastUsedAt: cloneTimePointer(record.LastUsedAt),
		ExpiresAt:  record.ExpiresAt,
		RevokedAt:  cloneTimePointer(record.RevokedAt),
	}
}

func generateAPITokenValue() (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	return "rtu_" + token, nil
}

func tokenIDFromValue(tokenValue string) string {
	sum := sha256.Sum256([]byte(tokenValue))
	return hex.EncodeToString(sum[:8])
}

func hashAPIToken(tokenValue string) string {
	sum := sha256.Sum256([]byte(tokenValue))
	return hex.EncodeToString(sum[:])
}

func normalizeTokenScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("at least one api token scope is required")
	}
	allowed := map[string]struct{}{
		APITokenScopeRunsRead:  {},
		APITokenScopeRunsWrite: {},
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(scopes))
	for _, raw := range scopes {
		scope := strings.TrimSpace(raw)
		if scope == "" {
			continue
		}
		if _, ok := allowed[scope]; !ok {
			return nil, fmt.Errorf("unsupported api token scope %q", scope)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("at least one api token scope is required")
	}
	sort.Strings(normalized)
	return normalized, nil
}

func cloneAPITokenRecord(record *APITokenRecord) *APITokenRecord {
	if record == nil {
		return nil
	}
	return &APITokenRecord{
		ID:         record.ID,
		Label:      record.Label,
		TokenHash:  record.TokenHash,
		Subject:    record.Subject,
		Claims:     cloneClaimsMap(record.Claims),
		Scopes:     append([]string(nil), record.Scopes...),
		CreatedAt:  record.CreatedAt,
		LastUsedAt: cloneTimePointer(record.LastUsedAt),
		ExpiresAt:  record.ExpiresAt,
		RevokedAt:  cloneTimePointer(record.RevokedAt),
	}
}

func cloneClaimsMap(claims map[string]interface{}) map[string]interface{} {
	if claims == nil {
		return map[string]interface{}{}
	}
	cloned := make(map[string]interface{}, len(claims))
	for key, value := range claims {
		switch typed := value.(type) {
		case []interface{}:
			cloned[key] = append([]interface{}(nil), typed...)
		case []string:
			items := make([]interface{}, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
			cloned[key] = items
		default:
			cloned[key] = typed
		}
	}
	return cloned
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
