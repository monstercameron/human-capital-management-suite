package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type ConfigurationKind string

const (
	ConfigCustomObjectType ConfigurationKind = "custom_object_type"
	ConfigCustomObject     ConfigurationKind = "custom_object"
	ConfigTenantParameter  ConfigurationKind = "tenant_parameter"
	ConfigSecret           ConfigurationKind = "secret"
	ConfigWorkflow         ConfigurationKind = "workflow_definition"
	ConfigRole             ConfigurationKind = "role"
	ConfigRoleBinding      ConfigurationKind = "role_binding"
	ConfigConnector        ConfigurationKind = "connector"
	ConfigConnection       ConfigurationKind = "connection"
	ConfigJobArchitecture  ConfigurationKind = "job_architecture"
	ConfigReportDefinition ConfigurationKind = "report_definition"
)

const (
	ConfigActive          = "ACTIVE"
	ConfigPendingApproval = "PENDING_APPROVAL"
	ConfigRetired         = "RETIRED"
)

var (
	ErrConfigInvalid    = errors.New("config: invalid configuration resource")
	ErrConfigNotFound   = errors.New("config: configuration resource not found")
	ErrConfigRevision   = errors.New("config: revision precondition failed")
	ErrConfigSecretRead = errors.New("config: secret values are write-only")
	ErrConfigApproval   = errors.New("config: approval is required")
)

type Configuration struct {
	TenantID        string
	ID              string
	Kind            ConfigurationKind
	Revision        uint64
	Status          string
	Value           []byte
	Secret          bool
	PendingApproval bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ConfigurationMutation struct {
	TenantID   string
	ID         string
	Kind       ConfigurationKind
	Value      []byte
	Secret     bool
	HighImpact bool
	IfMatch    uint64
}

type ConfigurationStore struct {
	mu    sync.RWMutex
	now   func() time.Time
	items map[string]Configuration
}

func NewConfigurationStore(now func() time.Time) *ConfigurationStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ConfigurationStore{now: now, items: make(map[string]Configuration)}
}

func (s *ConfigurationStore) Create(m ConfigurationMutation) (Configuration, error) {
	if err := validateConfigMutation(m); err != nil {
		return Configuration{}, err
	}
	if m.IfMatch != 0 {
		return Configuration{}, ErrConfigRevision
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := configKey(m.TenantID, m.ID)
	if _, ok := s.items[key]; ok {
		return Configuration{}, ErrConfigRevision
	}
	now := s.now().UTC()
	item := Configuration{TenantID: m.TenantID, ID: m.ID, Kind: m.Kind, Revision: 1, Status: ConfigActive, Value: cloneBytes(m.Value), Secret: m.Secret, PendingApproval: m.HighImpact, CreatedAt: now, UpdatedAt: now}
	if m.HighImpact {
		item.Status = ConfigPendingApproval
	}
	s.items[key] = item
	return projectConfiguration(item), nil
}

func (s *ConfigurationStore) Get(tenant, id string) (Configuration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[configKey(tenant, id)]
	if !ok {
		return Configuration{}, ErrConfigNotFound
	}
	return projectConfiguration(item), nil
}

func (s *ConfigurationStore) List(tenant string, kind ConfigurationKind) []Configuration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Configuration, 0)
	for _, item := range s.items {
		if item.TenantID == tenant && (kind == "" || item.Kind == kind) {
			out = append(out, projectConfiguration(item))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *ConfigurationStore) Update(m ConfigurationMutation) (Configuration, error) {
	if err := validateConfigMutation(m); err != nil {
		return Configuration{}, err
	}
	if m.IfMatch == 0 {
		return Configuration{}, ErrConfigRevision
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := configKey(m.TenantID, m.ID)
	item, ok := s.items[key]
	if !ok {
		return Configuration{}, ErrConfigNotFound
	}
	if item.Revision != m.IfMatch {
		return Configuration{}, fmt.Errorf("%w: expected %d actual %d", ErrConfigRevision, m.IfMatch, item.Revision)
	}
	if item.Status == ConfigRetired {
		return Configuration{}, ErrConfigRevision
	}
	item.Revision++
	item.Value = cloneBytes(m.Value)
	item.Secret = m.Secret || item.Secret
	item.PendingApproval = m.HighImpact
	item.Status = ConfigActive
	if item.PendingApproval {
		item.Status = ConfigPendingApproval
	}
	item.UpdatedAt = s.now().UTC()
	s.items[key] = item
	return projectConfiguration(item), nil
}

func (s *ConfigurationStore) Retire(tenant, id string, ifMatch uint64) (Configuration, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(id) == "" || ifMatch == 0 {
		return Configuration{}, ErrConfigInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := configKey(tenant, id)
	item, ok := s.items[key]
	if !ok {
		return Configuration{}, ErrConfigNotFound
	}
	if item.Revision != ifMatch {
		return Configuration{}, ErrConfigRevision
	}
	item.Revision++
	item.Status, item.PendingApproval, item.UpdatedAt = ConfigRetired, false, s.now().UTC()
	s.items[key] = item
	return projectConfiguration(item), nil
}

func projectConfiguration(item Configuration) Configuration {
	item.Value = cloneBytes(item.Value)
	if item.Secret {
		item.Value = nil
	}
	return item
}

func validateConfigMutation(m ConfigurationMutation) error {
	if strings.TrimSpace(m.TenantID) == "" || strings.TrimSpace(m.ID) == "" || !validConfigurationKind(m.Kind) {
		return ErrConfigInvalid
	}
	if len(m.Value) == 0 {
		return ErrConfigInvalid
	}
	return nil
}

func validConfigurationKind(kind ConfigurationKind) bool {
	switch kind {
	case ConfigCustomObjectType, ConfigCustomObject, ConfigTenantParameter, ConfigSecret, ConfigWorkflow, ConfigRole, ConfigRoleBinding, ConfigConnector, ConfigConnection, ConfigJobArchitecture, ConfigReportDefinition:
		return true
	default:
		return false
	}
}

func configKey(tenant, id string) string { return tenant + "\x00" + id }
func cloneBytes(value []byte) []byte     { return append([]byte(nil), value...) }

func ConfigurationDigest(item Configuration) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{item.TenantID, item.ID, string(item.Kind), fmt.Sprint(item.Revision), item.Status, hex.EncodeToString(item.Value)}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
