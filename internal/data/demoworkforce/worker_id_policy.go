package demoworkforce

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// SeedWorkerIDPolicy records the first-write-only worker-number policy for a
// demo company. The policy is derived from the same planned employees that
// seed creates, so a new reservation continues the company's existing IDs.
func (p *Pack) SeedWorkerIDPolicy(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) error {
	if p == nil || tx == nil || tenant == uuid.Nil {
		return fmt.Errorf("demoworkforce: invalid worker ID policy seed")
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	employees, err := p.Plan(tenant)
	if err != nil {
		return err
	}
	prefix, separator, digits, start, next, err := workerNumberRule(employees)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO organization_worker_id_policy (tenant_id,organization_scope_id,version,prefix,suffix,separator,sequence_digits,start_at,next_sequence,increment_by,zero_pad,year_format,include_unit_code,check_digit,excluded_ranges,updated_by) VALUES ($1,$2,1,$3,'',$4,$5,$6,$7,1,true,'NONE',false,'NONE','',$8) ON CONFLICT (tenant_id,organization_scope_id) DO NOTHING`, tenant, p.OrgScope(), prefix, separator, digits, start, next, p.Key+":demo-seed")
	if err != nil {
		return fmt.Errorf("insert worker ID policy: %w", err)
	}
	return nil
}

func workerNumberRule(employees []Employee) (prefix, separator string, digits int, start, next int64, err error) {
	if len(employees) == 0 {
		return "", "", 0, 0, 0, fmt.Errorf("demoworkforce: the pack has no workers")
	}
	var min, max int64
	for index, employee := range employees {
		number := strings.TrimSpace(employee.Row.WorkerNumber)
		cut := strings.LastIndex(number, "-")
		if cut <= 0 || cut == len(number)-1 {
			return "", "", 0, 0, 0, fmt.Errorf("demoworkforce: worker %d has invalid worker number %q", index+1, number)
		}
		candidatePrefix, candidateDigits := number[:cut], number[cut+1:]
		sequence, parseErr := strconv.ParseInt(candidateDigits, 10, 64)
		if parseErr != nil || sequence < 0 {
			return "", "", 0, 0, 0, fmt.Errorf("demoworkforce: worker %d has invalid numeric sequence in %q", index+1, number)
		}
		if index == 0 {
			prefix, separator, digits, min, max = candidatePrefix, "-", len(candidateDigits), sequence, sequence
			continue
		}
		if candidatePrefix != prefix || len(candidateDigits) != digits {
			return "", "", 0, 0, 0, fmt.Errorf("demoworkforce: worker number %q does not match %s-%0*d", number, prefix, digits, max)
		}
		if sequence < min {
			min = sequence
		}
		if sequence > max {
			max = sequence
		}
	}
	if max == int64(1<<63-1) {
		return "", "", 0, 0, 0, fmt.Errorf("demoworkforce: worker number sequence is exhausted")
	}
	return prefix, separator, digits, min, max + 1, nil
}
