// Protected leave extensions (WF-CAP-005): lookback eligibility over
// provider-aggregated hours of service, per-episode intermittent draws, and
// certification timing from versioned rule packs. Eligibility never reads
// caller-supplied hour facts; the provider is the only hours authority.
//
// The federal pack below is a versioned fixture mirroring the federal
// statutory shape (twelve months, 1250 hours, fifteen-day certification). It
// pins computation for GOLDEN tests; like medical_trace.go, nothing here
// certifies a real jurisdiction.
package leave

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidLookback    = errors.New("leave: invalid lookback window")
	ErrInvalidEpisode     = errors.New("leave: invalid intermittent episode")
	ErrDuplicateEpisode   = errors.New("leave: duplicate intermittent episode")
	ErrInvalidCertPack    = errors.New("leave: invalid certification rule pack")
	ErrInvalidCertRequest = errors.New("leave: invalid certification timing request")
)

// HoursOfServiceProvider aggregates worked time for one worker over a closed
// date range. Implementations read timekeeping truth; callers never supply
// hours beside the provider's answer.
type HoursOfServiceProvider interface {
	WorkedHours(ctx context.Context, workerID string, from, to values.LocalDate) (values.Decimal, error)
}

// Lookback is the closed twelve-month-style window [From, To] hours aggregate
// over.
type Lookback struct {
	From values.LocalDate
	To   values.LocalDate
}

// LookbackWindow derives the monthsBack-month window ending at asOf. Month
// ends clamp to the target month's last day, so a March 31 as-of looks back
// to a valid February date rather than failing.
func LookbackWindow(asOf values.LocalDate, monthsBack int) (Lookback, error) {
	if asOf.Validate() != nil {
		return Lookback{}, fmt.Errorf("%w: as-of date is invalid", ErrInvalidLookback)
	}
	if monthsBack < 1 || monthsBack > 1200 {
		return Lookback{}, fmt.Errorf("%w: months must be within 1..1200", ErrInvalidLookback)
	}
	total := int(asOf.Year())*12 + int(asOf.Month()) - 1 - monthsBack
	year, month0 := total/12, total%12
	day := int(asOf.Day())
	if last := daysInLeaveMonth(year, time.Month(month0+1)); day > last {
		day = last
	}
	from, err := values.NewLocalDate(year, time.Month(month0+1), day)
	if err != nil {
		return Lookback{}, fmt.Errorf("%w: %v", ErrInvalidLookback, err)
	}
	return Lookback{From: from, To: asOf}, nil
}

func daysInLeaveMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// ProviderHours aggregates the worker's hours over the lookback through the
// provider. A nil provider or an invalid answer fails closed: eligibility
// without hours is UNKNOWN, never denial and never approval.
func ProviderHours(ctx context.Context, provider HoursOfServiceProvider, workerID string, window Lookback) (values.Decimal, error) {
	if provider == nil {
		return values.Decimal{}, fmt.Errorf("%w: hours-of-service provider is required", ErrInvalidLookback)
	}
	if !workerIDPattern.MatchString(workerID) {
		return values.Decimal{}, fmt.Errorf("%w: worker_id", ErrInvalidLookback)
	}
	hours, err := provider.WorkedHours(ctx, workerID, window.From, window.To)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("%w: provider: %v", ErrInvalidLookback, err)
	}
	if err := hours.Validate(); err != nil {
		return values.Decimal{}, fmt.Errorf("%w: provider answer: %v", ErrInvalidLookback, err)
	}
	return hours, nil
}

// LookbackVerdict is the hours-and-service eligibility truth.
type LookbackVerdict struct {
	Result   string
	Blockers []string
}

// EvaluateLookbackEligibility compares provider hours and months of service
// against the rule pack thresholds. Missing service fails closed to
// INELIGIBLE with named blockers; caller facts are not an input.
func EvaluateLookbackEligibility(hours values.Decimal, monthsEmployed int, pack CertificationRulePack) LookbackVerdict {
	var blockers []string
	if hours.Cmp(pack.MinimumHours) < 0 {
		blockers = append(blockers, fmt.Sprintf("hours-of-service:%s-of-%s", hours.String(), pack.MinimumHours.String()))
	}
	if monthsEmployed < pack.MinimumEmploymentMonths {
		blockers = append(blockers, fmt.Sprintf("months-of-service:%d-of-%d", monthsEmployed, pack.MinimumEmploymentMonths))
	}
	if len(blockers) > 0 {
		return LookbackVerdict{Result: ProgramIneligible, Blockers: blockers}
	}
	return LookbackVerdict{Result: ProgramEligible}
}

// IntermittentEpisode is one dated absence within an intermittent leave: the
// unit that draws hours.
type IntermittentEpisode struct {
	EpisodeID string
	WorkerID  string
	Start     values.LocalDate
	End       values.LocalDate
	Hours     values.Decimal
}

func (e IntermittentEpisode) Validate() error {
	if strings.TrimSpace(e.EpisodeID) == "" {
		return fmt.Errorf("%w: episode_id is required", ErrInvalidEpisode)
	}
	if !workerIDPattern.MatchString(e.WorkerID) {
		return fmt.Errorf("%w: worker_id", ErrInvalidEpisode)
	}
	if e.Start.Validate() != nil || e.End.Validate() != nil || e.End.Compare(e.Start) < 0 {
		return fmt.Errorf("%w: start and end must be valid with end on or after start", ErrInvalidEpisode)
	}
	if err := e.Hours.Validate(); err != nil || e.Hours.Sign() <= 0 {
		return fmt.Errorf("%w: hours must be positive", ErrInvalidEpisode)
	}
	return nil
}

// EpisodeDraw is one episode's hours draw. The idempotency key derives from
// the worker and episode, so a retried episode draws once.
type EpisodeDraw struct {
	EpisodeID      string
	WorkerID       string
	Hours          values.Decimal
	IdempotencyKey string
}

// DrawEpisodes draws hours per episode instead of posting once at leave
// start. A foreign worker, an invalid episode or a repeated episode refuses
// the whole set: partial draws never post.
func DrawEpisodes(workerID string, episodes []IntermittentEpisode) ([]EpisodeDraw, error) {
	if !workerIDPattern.MatchString(workerID) {
		return nil, fmt.Errorf("%w: worker_id", ErrInvalidEpisode)
	}
	seen := make(map[string]bool, len(episodes))
	draws := make([]EpisodeDraw, 0, len(episodes))
	for _, episode := range episodes {
		if err := episode.Validate(); err != nil {
			return nil, err
		}
		if episode.WorkerID != workerID {
			return nil, fmt.Errorf("%w: episode %s belongs to %s", ErrInvalidEpisode, episode.EpisodeID, episode.WorkerID)
		}
		if seen[episode.EpisodeID] {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateEpisode, episode.EpisodeID)
		}
		seen[episode.EpisodeID] = true
		draws = append(draws, EpisodeDraw{EpisodeID: episode.EpisodeID, WorkerID: workerID, Hours: episode.Hours, IdempotencyKey: "leavedraw:" + workerID + ":" + episode.EpisodeID})
	}
	return draws, nil
}

// CertificationRulePack is the versioned timing source for medical
// certification: initial due dates, recertification intervals and the
// eligibility thresholds above.
type CertificationRulePack struct {
	PackRef                 string
	Version                 string
	InitialDueDays          int
	RecertIntervalDays      int
	MinimumEmploymentMonths int
	MinimumHours            values.Decimal
	FitnessForDutyRequired  bool
}

func (p CertificationRulePack) Validate() error {
	if strings.TrimSpace(p.PackRef) == "" || strings.TrimSpace(p.Version) == "" {
		return fmt.Errorf("%w: pack ref and version are required", ErrInvalidCertPack)
	}
	if p.InitialDueDays < 1 || p.RecertIntervalDays < 1 || p.MinimumEmploymentMonths < 0 {
		return fmt.Errorf("%w: timing thresholds must be positive", ErrInvalidCertPack)
	}
	if err := p.MinimumHours.Validate(); err != nil || p.MinimumHours.Sign() < 0 {
		return fmt.Errorf("%w: minimum hours must be valid", ErrInvalidCertPack)
	}
	return nil
}

// FederalCertificationPack is the versioned federal fixture pack.
func FederalCertificationPack() CertificationRulePack {
	return CertificationRulePack{
		PackRef: "leave.certification.federal/v1", Version: "2026.1",
		InitialDueDays: 15, RecertIntervalDays: 30,
		MinimumEmploymentMonths: 12, MinimumHours: values.MustDecimal("1250", 2, values.RoundingExactRequired),
	}
}

// CertificationDue returns the initial certification due date: the request
// date plus the pack's initial window.
func CertificationDue(requestDate values.LocalDate, pack CertificationRulePack) (values.LocalDate, error) {
	if err := pack.Validate(); err != nil {
		return values.LocalDate{}, err
	}
	if requestDate.Validate() != nil {
		return values.LocalDate{}, fmt.Errorf("%w: request date is invalid", ErrInvalidCertRequest)
	}
	return requestDate.AddDays(pack.InitialDueDays), nil
}

// Certification is one issued medical certification.
type Certification struct {
	Issued             values.LocalDate
	StatedDurationDays int
}

// RecertificationTrigger is the closed recertification vocabulary.
type RecertificationTrigger string

const (
	TriggerIntervalElapsed  RecertificationTrigger = "INTERVAL_ELAPSED"
	TriggerDurationExceeded RecertificationTrigger = "DURATION_EXCEEDED"
	TriggerFitnessForDuty   RecertificationTrigger = "FITNESS_FOR_DUTY"
)

func triggerNames(triggers []RecertificationTrigger) []string {
	out := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		out = append(out, string(trigger))
	}
	return out
}

func daysBetween(from, to values.LocalDate) int {
	start := time.Date(int(from.Year()), from.Month(), int(from.Day()), 0, 0, 0, 0, time.UTC)
	end := time.Date(int(to.Year()), to.Month(), int(to.Day()), 0, 0, 0, 0, time.UTC)
	return int(end.Sub(start).Hours() / 24)
}

// RecertificationTriggers evaluates the pack's recertification rules at asOf:
// the interval elapsed since issue, the certified duration exceeded, and the
// fitness-for-duty requirement when the pack states one and episodes accrued
// since certification. Triggers only accumulate with time.
func RecertificationTriggers(cert Certification, asOf values.LocalDate, episodesSinceCert int, pack CertificationRulePack) []RecertificationTrigger {
	if cert.Issued.Validate() != nil || asOf.Validate() != nil || pack.Validate() != nil {
		return nil
	}
	elapsed := daysBetween(cert.Issued, asOf)
	if elapsed < 0 {
		return nil
	}
	var triggers []RecertificationTrigger
	if elapsed >= pack.RecertIntervalDays {
		triggers = append(triggers, TriggerIntervalElapsed)
	}
	if cert.StatedDurationDays > 0 && elapsed > cert.StatedDurationDays {
		triggers = append(triggers, TriggerDurationExceeded)
	}
	if pack.FitnessForDutyRequired && episodesSinceCert > 0 {
		triggers = append(triggers, TriggerFitnessForDuty)
	}
	return triggers
}

// CertificationGoldenSummary is the pinned federal-pack computation.
type CertificationGoldenSummary struct {
	InitialDue string
	RecertDue  string
	Triggers   []string
	Digest     string
}

// GoldenCertificationSummary derives the initial due, the recertification due
// and the triggers at asOf, sealed with a digest.
func GoldenCertificationSummary(pack CertificationRulePack, requestDate values.LocalDate, cert Certification, asOf values.LocalDate) (CertificationGoldenSummary, error) {
	due, err := CertificationDue(requestDate, pack)
	if err != nil {
		return CertificationGoldenSummary{}, err
	}
	recert := cert.Issued.AddDays(pack.RecertIntervalDays)
	triggers := RecertificationTriggers(cert, asOf, 1, pack)
	sum := sha256.Sum256([]byte(strings.Join([]string{"leave-certification-golden", pack.PackRef, pack.Version, due.String(), recert.String(), strings.Join(triggerNames(triggers), ",")}, "\x00")))
	return CertificationGoldenSummary{InitialDue: due.String(), RecertDue: recert.String(), Triggers: triggerNames(triggers), Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}
