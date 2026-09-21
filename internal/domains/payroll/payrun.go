package payroll

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/filing"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrPayRun is the REV-021-02 sentinel: any leg of the wage-tax-filing chain
// that refuses fails the whole period run with this error.
var ErrPayRun = errors.New("payroll: pay run failed")

// FilingTerms carries the report identity a FilingPackage seals. Amounts and
// digests always come from the wage and tax legs; these terms only name the
// report being produced.
type FilingTerms struct {
	ReportDefinition string
	ReportVersion    string
	SchemaVersion    string
	PeriodRef        string
	Jurisdiction     string
	SourceWatermark  string
	SignerRef        string
	ApprovalRef      string
	IdempotencyKey   string
}

// PayRunInput is one worker's governed period close: the priced workweek,
// the tax version pin and profile references, and the filing report terms.
type PayRunInput struct {
	Wage             WageInput
	PeriodStart      time.Time
	PeriodEnd        time.Time
	PreTaxDeductions values.Decimal
	TaxProfileRef    string
	RegistrationRef  string
	ElectionRef      string
	WageBasisRef     string
	Pack             TaxRulePack
	PriorAccumulator values.Decimal
	Filing           FilingTerms
}

// PayRunResult is the composed WAGE-001, TAX-001 and FILING-001 answer for
// one worker's period: the wage result feeds the tax input, and both digests
// feed the filing package's source identity.
type PayRunResult struct {
	Wage              WageResult
	Tax               TaxResult
	Package           filing.FilingPackage
	SourceValueDigest string
}

// SourceDigest binds the wage and tax digests into the filing source
// identity. It is exported so callers and tests can verify a package traces
// back to exactly the wage and tax results that produced it.
func SourceDigest(wageDigest, taxDigest string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{wageDigest, taxDigest}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// RunPayrollPeriod closes one worker's period end to end: EvaluateWages
// prices the approved workweek, CalculateTax prices the resulting gross
// under the pinned pack, and BuildFilingPackage seals a package whose source
// values trace back to both. A refusal on any leg fails the run with
// ErrPayRun; no partial chain is ever returned.
func RunPayrollPeriod(input PayRunInput) (PayRunResult, error) {
	wage, err := EvaluateWages(input.Wage)
	if err != nil {
		return PayRunResult{}, fmt.Errorf("%w: wage leg: %v", ErrPayRun, err)
	}
	if wage.Status != WageOK {
		return PayRunResult{}, fmt.Errorf("%w: wage leg status %s is not payable", ErrPayRun, wage.Status)
	}
	gross, err := values.NewDecimal(wage.GrossPay.String(), 2, values.RoundingHalfUp)
	if err != nil {
		return PayRunResult{}, fmt.Errorf("%w: wage gross %q is not cent-priced: %v", ErrPayRun, wage.GrossPay, err)
	}
	tax, err := CalculateTax(TaxInput{
		Tenant:           input.Wage.Tenant,
		WorkerRef:        input.Wage.WorkerRef,
		PeriodStart:      input.PeriodStart,
		PeriodEnd:        input.PeriodEnd,
		GrossWages:       gross,
		PreTaxDeductions: input.PreTaxDeductions,
		TaxProfileRef:    input.TaxProfileRef,
		RegistrationRef:  input.RegistrationRef,
		ElectionRef:      input.ElectionRef,
		WageBasisRef:     input.WageBasisRef,
		RuleVersion:      input.Pack.Version,
	}, input.Pack, input.PriorAccumulator)
	if err != nil {
		return PayRunResult{}, fmt.Errorf("%w: tax leg: %v", ErrPayRun, err)
	}
	source := SourceDigest(wage.Digest, tax.Digest)
	rendered := SourceDigest(source, "rendered")
	submission := SourceDigest(source, "submission")
	pkg, err := filing.BuildFilingPackage(filing.FilingPackageInput{
		Tenant:            input.Wage.Tenant,
		ReportDefinition:  input.Filing.ReportDefinition,
		ReportVersion:     input.Filing.ReportVersion,
		SchemaVersion:     input.Filing.SchemaVersion,
		PeriodRef:         input.Filing.PeriodRef,
		Jurisdiction:      input.Filing.Jurisdiction,
		SourceValueDigest: source,
		SourceWatermark:   input.Filing.SourceWatermark,
		RenderedHash:      rendered,
		SubmissionHash:    submission,
		SignerRef:         input.Filing.SignerRef,
		ApprovalRef:       input.Filing.ApprovalRef,
		IdempotencyKey:    input.Filing.IdempotencyKey,
	})
	if err != nil {
		return PayRunResult{}, fmt.Errorf("%w: filing leg: %v", ErrPayRun, err)
	}
	return PayRunResult{Wage: wage, Tax: tax, Package: pkg, SourceValueDigest: source}, nil
}
