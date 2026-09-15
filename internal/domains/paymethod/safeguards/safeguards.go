// Package safeguards owns the pure, versioned GLBA Safeguards Rule (16 CFR
// 314) information-security program record. It maps covered tenants to
// their risk assessment, service-provider oversight, access-control,
// encryption and monitoring evidence, incident history, and the
// reproducible consumer count driving the FTC 500-consumer notice
// decision. It performs no I/O and consults no clock, database, or
// provider; all inputs arrive from the caller.
package safeguards

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const schemaVersion = 1

// Version returns the schema revision of this package's public contract.
func Version() int { return schemaVersion }

// NoticeThresholdConsumers is the FTC 500-consumer breach-notification
// threshold (2023 Safeguards Rule amendment).
const NoticeThresholdConsumers = 500

var (
	ErrInvalidProgram = errors.New("safeguards: invalid information-security program")
	ErrMissingElement = errors.New("safeguards: covered program missing required element")
	ErrUnsatisfactory = errors.New("safeguards: unsatisfactory service-provider diligence")
	ErrNegativeCount  = errors.New("safeguards: negative consumer count")
	ErrMissingMethod  = errors.New("safeguards: missing consumer-count method")
	ErrMissingTenant  = errors.New("safeguards: missing tenant")
	ErrMissingReview  = errors.New("safeguards: missing review date")
)

// ProviderDiligence records one service-provider oversight review. Ref
// fields are evidence pointers, never credentials or account data.
type ProviderDiligence struct {
	Name         string
	ReviewRef    string
	Satisfactory bool
}

// IncidentRecord notes one security event and whether notice was given.
type IncidentRecord struct {
	ID       string
	Notified bool
}

// ProgramArgs are the caller-supplied inputs for one program record.
type ProgramArgs struct {
	Tenant              string
	CoveredInstitution  bool
	RiskAssessmentRef   string
	ServiceProviders    []ProviderDiligence
	AccessControlRef    string
	EncryptionRef       string
	MonitoringRef       string
	IncidentRecords     []IncidentRecord
	ConsumerCount       int
	ConsumerCountMethod string
	ReviewDate          string
}

// Program is the validated, versioned Safeguards program record.
type Program struct {
	Tenant              string
	CoveredInstitution  bool
	RiskAssessmentRef   string
	ServiceProviders    []ProviderDiligence
	AccessControlRef    string
	EncryptionRef       string
	MonitoringRef       string
	IncidentRecords     []IncidentRecord
	ConsumerCount       int
	ConsumerCountMethod string
	ReviewDate          string
}

// NewProgram validates args and returns the immutable program record.
// Non-covered tenants resolve an explicit not-applicable record without
// further elements; covered institutions must name every element.
func NewProgram(a ProgramArgs) (Program, error) {
	if strings.TrimSpace(a.Tenant) == "" {
		return Program{}, fmt.Errorf("%w: tenant", ErrMissingTenant)
	}
	if !a.CoveredInstitution {
		return Program{Tenant: a.Tenant}, nil
	}
	if strings.TrimSpace(a.RiskAssessmentRef) == "" {
		return Program{}, fmt.Errorf("%w: risk assessment", ErrMissingElement)
	}
	if len(a.ServiceProviders) == 0 {
		return Program{}, fmt.Errorf("%w: service-provider oversight", ErrMissingElement)
	}
	for _, p := range a.ServiceProviders {
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.ReviewRef) == "" {
			return Program{}, fmt.Errorf("%w: provider review ref", ErrMissingElement)
		}
		if !p.Satisfactory {
			return Program{}, fmt.Errorf("%w: %s", ErrUnsatisfactory, p.Name)
		}
	}
	if strings.TrimSpace(a.AccessControlRef) == "" ||
		strings.TrimSpace(a.EncryptionRef) == "" ||
		strings.TrimSpace(a.MonitoringRef) == "" {
		return Program{}, fmt.Errorf("%w: access-control/encryption/monitoring evidence", ErrMissingElement)
	}
	if a.ConsumerCount < 0 {
		return Program{}, ErrNegativeCount
	}
	if strings.TrimSpace(a.ConsumerCountMethod) == "" {
		return Program{}, ErrMissingMethod
	}
	if strings.TrimSpace(a.ReviewDate) == "" {
		return Program{}, fmt.Errorf("%w: review date", ErrMissingReview)
	}
	providers := append([]ProviderDiligence(nil), a.ServiceProviders...)
	incidents := append([]IncidentRecord(nil), a.IncidentRecords...)
	return Program{
		Tenant:              a.Tenant,
		CoveredInstitution:  true,
		RiskAssessmentRef:   a.RiskAssessmentRef,
		ServiceProviders:    providers,
		AccessControlRef:    a.AccessControlRef,
		EncryptionRef:       a.EncryptionRef,
		MonitoringRef:       a.MonitoringRef,
		IncidentRecords:     incidents,
		ConsumerCount:       a.ConsumerCount,
		ConsumerCountMethod: a.ConsumerCountMethod,
		ReviewDate:          a.ReviewDate,
	}, nil
}

// Validate re-checks a constructed record's invariants.
func (p Program) Validate() error {
	if strings.TrimSpace(p.Tenant) == "" {
		return fmt.Errorf("%w: tenant", ErrMissingTenant)
	}
	if !p.CoveredInstitution {
		return nil
	}
	if _, err := NewProgram(ProgramArgs{
		Tenant:              p.Tenant,
		CoveredInstitution:  true,
		RiskAssessmentRef:   p.RiskAssessmentRef,
		ServiceProviders:    p.ServiceProviders,
		AccessControlRef:    p.AccessControlRef,
		EncryptionRef:       p.EncryptionRef,
		MonitoringRef:       p.MonitoringRef,
		IncidentRecords:     p.IncidentRecords,
		ConsumerCount:       p.ConsumerCount,
		ConsumerCountMethod: p.ConsumerCountMethod,
		ReviewDate:          p.ReviewDate,
	}); err != nil {
		return err
	}
	return nil
}

// NoticeRequired reports the FTC 500-consumer notice decision as a pure
// function of the reproducible consumer count. Not-applicable programs
// never require notice.
func (p Program) NoticeRequired() bool {
	if !p.CoveredInstitution {
		return false
	}
	return p.ConsumerCount >= NoticeThresholdConsumers
}

// Digest returns a reproducible, non-disclosing fingerprint of the
// consumer-count calculation and program elements. It carries hashes of
// references and counts, never raw tenant markers or consumer data.
func (p Program) Digest() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"v1",
		p.RiskAssessmentRef,
		p.AccessControlRef,
		p.EncryptionRef,
		p.MonitoringRef,
		p.ConsumerCountMethod,
		p.ReviewDate,
		fmt.Sprintf("%d:%d:%d", p.ConsumerCount, len(p.ServiceProviders), len(p.IncidentRecords)),
	}, "\x00")))
	return "saf-" + hex.EncodeToString(sum[:8])
}
