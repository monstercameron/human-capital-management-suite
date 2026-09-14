package messaging

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Governed bulk communications (MSG-012) send one accepted message intent
// to a frozen audience with per-recipient outcomes. The audience freezes
// at schedule time: every recipient is named, duplicates are refused
// rather than silently dropped, and any later mutation of the frozen set
// is rejected by digest. Results record per recipient, batches resume
// from a cursor, pause and kill take effect immediately, and
// reconciliation names every pending recipient and every failure, so an
// aggregate success can never hide one.
//
// One-person and bulk capabilities stay distinct: this file never sends
// anything itself. It governs the campaign a delivery provider executes,
// and the ledger it reports against.

// Bulk errors.
var (
	ErrInvalidBulkCampaign = errors.New("messaging: invalid bulk campaign")
	ErrBulkState           = errors.New("messaging: bulk campaign state forbids the recording")
)

// BulkRecipientStatus names one recipient's delivery outcome.
type BulkRecipientStatus string

// Per-recipient outcomes.
const (
	BulkQueued  BulkRecipientStatus = "QUEUED"
	BulkSent    BulkRecipientStatus = "SENT"
	BulkFailed  BulkRecipientStatus = "FAILED"
	BulkSkipped BulkRecipientStatus = "SKIPPED_EXCLUDED"
)

// BulkCampaignState names the campaign lifecycle.
type BulkCampaignState string

// Campaign states.
const (
	BulkDraft    BulkCampaignState = "DRAFT"
	BulkSending  BulkCampaignState = "SENDING"
	BulkPaused   BulkCampaignState = "PAUSED"
	BulkKilled   BulkCampaignState = "KILLED"
	BulkComplete BulkCampaignState = "COMPLETE"
)

// FrozenAudience is the immutable recipient set a campaign sends to.
type FrozenAudience struct {
	Recipients []string `json:"recipients"`
	Excluded   []string `json:"excluded"`
	Digest     string   `json:"digest"`
}

// BulkPreviewRequest asks what a campaign would cost and reach.
type BulkPreviewRequest struct {
	TenantID              string
	Intent                MessageIntent
	Candidates            []string
	Exclusions            []string
	CostPerRecipientCents int64
	RatePerMinute         int64
}

// BulkPreview reports reach, exclusions, cost and rate before scheduling.
type BulkPreview struct {
	Recipients         int
	Excluded           int
	EstimatedCostCents int64
	RatePerMinute      int64
	MinutesAtRate      int64
}

// PreviewBulk validates the intent and prices the frozen reach. It
// schedules nothing.
func PreviewBulk(req BulkPreviewRequest) (BulkPreview, error) {
	if err := req.Intent.Validate(); err != nil {
		return BulkPreview{}, err
	}
	recipients, excluded, err := freezeAudience(req.Candidates, req.Exclusions)
	if err != nil {
		return BulkPreview{}, err
	}
	if req.RatePerMinute <= 0 {
		return BulkPreview{}, fmt.Errorf("%w: rate per minute must be positive", ErrInvalidBulkCampaign)
	}
	if req.CostPerRecipientCents < 0 {
		return BulkPreview{}, fmt.Errorf("%w: cost per recipient cannot be negative", ErrInvalidBulkCampaign)
	}
	minutes := (int64(len(recipients)) + req.RatePerMinute - 1) / req.RatePerMinute
	return BulkPreview{
		Recipients: len(recipients), Excluded: len(excluded),
		EstimatedCostCents: int64(len(recipients)) * req.CostPerRecipientCents,
		RatePerMinute:      req.RatePerMinute, MinutesAtRate: minutes,
	}, nil
}

// BulkScheduleRequest schedules one governed campaign.
type BulkScheduleRequest struct {
	TenantID   string
	CampaignID string
	Intent     MessageIntent
	Audience   []string
	Exclusions []string
	BatchSize  int
}

// BulkCampaign is the scheduled, frozen campaign.
type BulkCampaign struct {
	TenantID   string
	CampaignID string
	Intent     MessageIntent
	Audience   FrozenAudience
	BatchSize  int
	State      BulkCampaignState
}

// ScheduleBulk freezes the audience and opens the campaign in SENDING.
// Duplicate recipients are refused: silent dedupe would rewrite who the
// campaign claims to reach.
func ScheduleBulk(req BulkScheduleRequest) (BulkCampaign, error) {
	if strings.TrimSpace(req.TenantID) == "" {
		return BulkCampaign{}, fmt.Errorf("%w: tenant is required", ErrInvalidBulkCampaign)
	}
	if strings.TrimSpace(req.CampaignID) == "" {
		return BulkCampaign{}, fmt.Errorf("%w: campaign id is required", ErrInvalidBulkCampaign)
	}
	if req.Intent.TenantID != req.TenantID {
		return BulkCampaign{}, fmt.Errorf("%w: intent tenant does not match the campaign tenant", ErrInvalidBulkCampaign)
	}
	if err := req.Intent.Validate(); err != nil {
		return BulkCampaign{}, err
	}
	if req.BatchSize <= 0 {
		return BulkCampaign{}, fmt.Errorf("%w: batch size must be positive", ErrInvalidBulkCampaign)
	}
	recipients, excluded, err := freezeAudience(req.Audience, req.Exclusions)
	if err != nil {
		return BulkCampaign{}, err
	}
	if len(recipients) == 0 {
		return BulkCampaign{}, fmt.Errorf("%w: frozen audience is empty", ErrInvalidBulkCampaign)
	}
	return BulkCampaign{
		TenantID: req.TenantID, CampaignID: req.CampaignID, Intent: req.Intent,
		Audience:  FrozenAudience{Recipients: recipients, Excluded: excluded, Digest: digestAudience(recipients, excluded)},
		BatchSize: req.BatchSize, State: BulkSending,
	}, nil
}

func freezeAudience(candidates, exclusions []string) ([]string, []string, error) {
	if len(candidates) == 0 {
		return nil, nil, fmt.Errorf("%w: audience candidates are required", ErrInvalidBulkCampaign)
	}
	excluded := map[string]bool{}
	for _, e := range exclusions {
		if strings.TrimSpace(e) == "" {
			return nil, nil, fmt.Errorf("%w: blank exclusion", ErrInvalidBulkCampaign)
		}
		excluded[e] = true
	}
	seen := map[string]bool{}
	var recipients, skipped []string
	for _, c := range candidates {
		if strings.TrimSpace(c) == "" {
			return nil, nil, fmt.Errorf("%w: blank recipient", ErrInvalidBulkCampaign)
		}
		if seen[c] {
			return nil, nil, fmt.Errorf("%w: duplicate recipient %q", ErrInvalidBulkCampaign, c)
		}
		seen[c] = true
		if excluded[c] {
			skipped = append(skipped, c)
			continue
		}
		recipients = append(recipients, c)
	}
	sort.Strings(recipients)
	sort.Strings(skipped)
	return recipients, skipped, nil
}

func digestAudience(recipients, excluded []string) string {
	h := sha256.New()
	for _, r := range recipients {
		h.Write([]byte("to\x00" + r + "\x00"))
	}
	for _, e := range excluded {
		h.Write([]byte("ex\x00" + e + "\x00"))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// BulkResult is one recipient's reported outcome.
type BulkResult struct {
	Recipient string
	Status    BulkRecipientStatus
	Attempt   int
	Failure   string
}

// BulkReport reconciles the frozen audience against recorded outcomes.
type BulkReport struct {
	CampaignID string
	State      BulkCampaignState
	Sent       int
	Failed     int
	Skipped    int
	Pending    []string
	Failures   []BulkResult
	Complete   bool
}

// BulkLedger records per-recipient outcomes for campaigns. It is safe for
// concurrent use; recordings serialize on the campaign.
type BulkLedger struct {
	mu      sync.Mutex
	results map[string]map[string]BulkResult
	states  map[string]BulkCampaignState
	digests map[string]string
}

// NewBulkLedger returns an empty ledger.
func NewBulkLedger() *BulkLedger {
	return &BulkLedger{results: map[string]map[string]BulkResult{}, states: map[string]BulkCampaignState{}, digests: map[string]string{}}
}

// Record stores one recipient outcome. The campaign's frozen digest must
// match the scheduled one, and recordings stop at pause or kill.
func (l *BulkLedger) Record(campaign BulkCampaign, result BulkResult) error {
	if result.Recipient == "" {
		return fmt.Errorf("%w: recipient is required", ErrInvalidBulkCampaign)
	}
	switch result.Status {
	case BulkSent, BulkFailed, BulkSkipped:
	default:
		return fmt.Errorf("%w: unrecordable status %q", ErrInvalidBulkCampaign, result.Status)
	}
	if result.Status == BulkFailed && strings.TrimSpace(result.Failure) == "" {
		return fmt.Errorf("%w: failures name their cause", ErrInvalidBulkCampaign)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if known, ok := l.digests[campaign.CampaignID]; ok && known != campaign.Audience.Digest {
		return fmt.Errorf("%w: audience digest does not match the scheduled freeze", ErrInvalidBulkCampaign)
	}
	l.digests[campaign.CampaignID] = campaign.Audience.Digest
	state := l.states[campaign.CampaignID]
	if state == "" {
		state = campaign.State
		l.states[campaign.CampaignID] = state
	}
	if state == BulkPaused {
		return fmt.Errorf("%w: campaign is paused", ErrBulkState)
	}
	if state == BulkKilled {
		return fmt.Errorf("%w: campaign was killed", ErrBulkState)
	}
	if !audienceContains(campaign.Audience.Recipients, result.Recipient) {
		return fmt.Errorf("%w: recipient is outside the frozen audience", ErrInvalidBulkCampaign)
	}
	if l.results[campaign.CampaignID] == nil {
		l.results[campaign.CampaignID] = map[string]BulkResult{}
	}
	l.results[campaign.CampaignID][result.Recipient] = result
	return nil
}

// Pause stops recordings until Resume. Kill ends the campaign: recordings
// after a kill are refused and unrecorded recipients stay explicitly
// unkilled, never silently completed.
func (l *BulkLedger) Pause(campaignID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.states[campaignID] = BulkPaused
}

// Resume reopens a paused campaign at its recorded cursor.
func (l *BulkLedger) Resume(campaignID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.states[campaignID] == BulkPaused {
		l.states[campaignID] = BulkSending
	}
}

// Kill ends the campaign immediately.
func (l *BulkLedger) Kill(campaignID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.states[campaignID] = BulkKilled
}

// Reconcile reports every frozen recipient's outcome: sent, failed with
// causes, skipped exclusions and the pending remainder. A campaign is
// complete only when nothing is pending.
func (l *BulkLedger) Reconcile(campaign BulkCampaign) (BulkReport, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if known, ok := l.digests[campaign.CampaignID]; ok && known != campaign.Audience.Digest {
		return BulkReport{}, fmt.Errorf("%w: audience digest does not match the scheduled freeze", ErrInvalidBulkCampaign)
	}
	state := l.states[campaign.CampaignID]
	if state == "" {
		state = campaign.State
	}
	report := BulkReport{CampaignID: campaign.CampaignID, State: state, Skipped: len(campaign.Audience.Excluded)}
	recorded := l.results[campaign.CampaignID]
	for _, r := range campaign.Audience.Recipients {
		res, ok := recorded[r]
		if !ok {
			report.Pending = append(report.Pending, r)
			continue
		}
		switch res.Status {
		case BulkSent:
			report.Sent++
		case BulkFailed:
			report.Failed++
			report.Failures = append(report.Failures, res)
		case BulkSkipped:
			report.Skipped++
		}
	}
	sort.Strings(report.Pending)
	sort.Slice(report.Failures, func(i, j int) bool { return report.Failures[i].Recipient < report.Failures[j].Recipient })
	report.Complete = len(report.Pending) == 0 && state != BulkKilled
	return report, nil
}

func audienceContains(recipients []string, want string) bool {
	for _, r := range recipients {
		if r == want {
			return true
		}
	}
	return false
}
