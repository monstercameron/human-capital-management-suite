package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// ChattoneSettingsStore keeps a workspace's writing-style choice. The
// document is the administrator's registry (ChattoneAdminStyle, instructions
// included); *chatstore.Store implements it.
type ChattoneSettingsStore interface {
	LoadWritingStyleSetting(context.Context, string) (chatstore.WritingStyleSetting, error)
	SaveWritingStyleSetting(context.Context, string, string, bool, json.RawMessage) (int64, error)
}

// ensureSettings reads the workspace's saved setting into the registry the
// first time the workspace is used by this process, and never again: from then
// on the registry is current, because every administrator save writes the store
// first and the registry second. A setting that cannot be read, or that is not
// a valid registry, leaves the workspace off (the caller reports unavailable)
// rather than falling back to a default that an administrator may have turned
// off. A workspace with no saved setting keeps whatever the composition root
// configured, which for every workspace but the development one is "off".
func (s *ChattoneService) ensureSettings(ctx context.Context, tenant string) error {
	if s == nil || s.Settings == nil || s.Rewrite == nil || s.Rewrite.Registry == nil {
		return nil
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.settingsLoaded[tenant] {
		return nil
	}
	saved, err := s.Settings.LoadWritingStyleSetting(ctx, tenant)
	if err != nil {
		return fmt.Errorf("%w: writing style setting: %v", chatrewrite.ErrUnavailable, err)
	}
	if saved.Found {
		var rows []ChattoneAdminStyle
		if err := json.Unmarshal(saved.Styles, &rows); err != nil {
			return fmt.Errorf("%w: writing style setting is not a registry", chatrewrite.ErrUnavailable)
		}
		styles := make([]chatrewrite.Style, len(rows))
		for i, row := range rows {
			styles[i] = chatrewrite.Style{ID: row.ID, Label: row.Label, Instruction: row.Instruction, Register: row.Register}
		}
		if err := s.Rewrite.Registry.Configure(tenant, saved.Enabled, styles); err != nil {
			return fmt.Errorf("%w: writing style setting is not a valid registry", chatrewrite.ErrUnavailable)
		}
	}
	if s.settingsLoaded == nil {
		s.settingsLoaded = map[string]bool{}
	}
	s.settingsLoaded[tenant] = true
	return nil
}

// saveSettings validates, writes and then configures; a failed write changes
// nothing in memory.
func (s *ChattoneService) saveSettings(ctx context.Context, tenant, by string, enabled bool, rows []ChattoneAdminStyle) error {
	styles := make([]chatrewrite.Style, len(rows))
	for i, row := range rows {
		styles[i] = chatrewrite.Style{ID: row.ID, Label: row.Label, Instruction: row.Instruction, Register: row.Register}
	}
	if err := chatrewrite.ValidateConfiguration(tenant, styles); err != nil {
		return err
	}
	if s.Settings != nil {
		document, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		if _, err := s.Settings.SaveWritingStyleSetting(ctx, tenant, by, enabled, document); err != nil {
			return fmt.Errorf("%w: %v", chatrewrite.ErrUnavailable, err)
		}
	}
	return s.Rewrite.Registry.Configure(tenant, enabled, styles)
}

// ChattoneUsageStore is the durable per-person daily count and usage line;
// *chatstore.Store implements it.
type ChattoneUsageStore interface {
	ReserveWritingStyleUse(context.Context, string, string, time.Time, int) error
	RecordWritingStyleUse(context.Context, string, string, time.Time, string, int, bool) error
}

// ChattoneDurableLedger is the production chatrewrite.Ledger: the per-person
// daily limit and every operation's usage line are written to the chat store,
// so they survive a restart and are shared by every process serving the
// workspace. A spent day is chatrewrite.ErrLimit; a store that cannot answer is
// an error, never a free call.
type ChattoneDurableLedger struct {
	Store ChattoneUsageStore
	Limit int
}

func (l ChattoneDurableLedger) Reserve(ctx context.Context, id chatrewrite.Identity, now time.Time) error {
	if ctx == nil || isNilPersonaOutputPort(l.Store) || !id.Valid() || l.Limit <= 0 {
		return chatrewrite.ErrUnavailable
	}
	if err := l.Store.ReserveWritingStyleUse(ctx, id.Tenant, id.Person, now, l.Limit); err != nil {
		if errors.Is(err, chatstore.ErrWritingStyleLimit) {
			return chatrewrite.ErrLimit
		}
		return fmt.Errorf("%w: %v", chatrewrite.ErrUnavailable, err)
	}
	return nil
}

func (l ChattoneDurableLedger) Record(ctx context.Context, u chatrewrite.Usage) error {
	if ctx == nil || isNilPersonaOutputPort(l.Store) || !u.Identity.Valid() {
		return chatrewrite.ErrUnavailable
	}
	if err := l.Store.RecordWritingStyleUse(ctx, u.Identity.Tenant, u.Identity.Person, u.At, u.Operation, u.Attempt, u.Succeeded); err != nil {
		return fmt.Errorf("%w: %v", chatrewrite.ErrUnavailable, err)
	}
	return nil
}

var (
	_ chatrewrite.Ledger    = ChattoneDurableLedger{}
	_ ChattoneSettingsStore = (*chatstore.Store)(nil)
	_ ChattoneUsageStore    = (*chatstore.Store)(nil)
)
