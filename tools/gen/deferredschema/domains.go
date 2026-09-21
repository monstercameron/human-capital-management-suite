package deferredschema

// Column is one domain-specific column beyond the tenant/id/bitemporal
// columns every table in this package renders automatically.
type Column struct {
	Name    string
	Type    string
	Default string // empty means no DEFAULT clause
}

// TableSpec describes one preview table. A domain contributes exactly two:
// a Head (OPERATIONAL, mutable current state) and an Evidence (PERMANENT,
// append-only revision/ledger trail referencing the Head by tenant+id).
type TableSpec struct {
	// Table is the snake_case table name.
	Table string
	// IDColumn is this table's own identity column (besides tenant_id).
	IDColumn string
	// KeyColumn, Head tables only: the human-facing unique key column.
	KeyColumn string
	// ParentTable/ParentIDColumn, Evidence tables only: the Head table and
	// column this table's ParentIDColumn foreign-keys to.
	ParentTable    string
	ParentIDColumn string
	// AppendOnly marks a PERMANENT, forbid_mutation-guarded table.
	AppendOnly bool
	// DataRole is the storage-disposition data_role (registry.RoleAggregate
	// for every table this package generates today).
	DataRole string
	// RetentionClass is OPERATIONAL for a Head table, PERMANENT for Evidence.
	RetentionClass string
	// Columns are the domain-specific columns, rendered in order after the
	// shared tenant/id/key columns and before the shared bitemporal columns.
	Columns []Column
	// CheckConstraints are extra table-level CHECK clauses beyond the shared
	// knowledge-order and (for Evidence) revision-positive checks.
	CheckConstraints []string
	// Notes documents the table for the disposition preview row.
	Notes string
}

// Domain is one future HCM domain: a Head/Evidence table pair plus the
// planning/data/models citations the entities came from.
type Domain struct {
	// Number fixes this domain's preview file ordinal (001..010) and its
	// position in the GREEN clause's domain list order.
	Number int
	// Slug names the domain in file names and owner-package previews
	// (internal/domains/<slug>, a package that does not exist yet).
	Slug string
	// Title is the human-readable domain name for comments.
	Title string
	// SourceDoc is the planning/data/models file the entities were read from.
	SourceDoc string
	// SourceSection is the "## " heading within SourceDoc the entities live
	// under.
	SourceSection string
	// Disposition is DRAFT or CONFORMANCE (see doc.go's disposition rule).
	Disposition string
	// DispositionReason documents why, for the preview row's notes.
	DispositionReason string
	Head              TableSpec
	Evidence          TableSpec
}

// Domains returns the ten future-domain definitions DB-016's GREEN clause
// names, in the fixed order the todo lists them: payroll, benefits, time,
// leave, recruiting, talent, learning, case, access, regulatory. The entity
// names and fields cited in each table's Notes/Columns are read from the
// planning/data/models sections named in SourceDoc/SourceSection.
func Domains() []Domain {
	return []Domain{
		payrollDomain(),
		benefitsDomain(),
		timeDomain(),
		leaveDomain(),
		recruitingDomain(),
		talentDomain(),
		learningDomain(),
		caseDomain(),
		accessDomain(),
		regulatoryDomain(),
	}
}

const notFunded = "no funded materialize-domain todo exists yet in planning/todos.md; pure exploratory vocabulary per planning/data/models/README.md's coverage rule"

func payrollDomain() Domain {
	return Domain{
		Number:            1,
		Slug:              "payroll",
		Title:             "Payroll",
		SourceDoc:         "planning/data/models/rewards-payroll-workforce.md",
		SourceSection:     "## Payroll",
		Disposition:       "DRAFT",
		DispositionReason: notFunded,
		Head: TableSpec{
			Table:          "payroll_run_preview",
			IDColumn:       "run_id",
			KeyColumn:      "run_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "pay_group_ref", Type: "text"},
				{Name: "period_ref", Type: "text"},
				{Name: "run_type", Type: "text"},
				{Name: "calculation_version", Type: "text"},
				{Name: "status", Type: "text", Default: "'PREPARING'"},
			},
			CheckConstraints: []string{
				"status IN ('PREPARING', 'CALCULATING', 'EXCEPTION', 'APPROVED', 'RELEASED')",
			},
			Notes: "Preview of PayrollRun (planning/data/models/rewards-payroll-workforce.md ## Payroll): one row per payroll run's population/period/run-type identity and current lifecycle status.",
		},
		Evidence: TableSpec{
			Table:          "payroll_ledger_entry",
			IDColumn:       "entry_id",
			ParentTable:    "payroll_run_preview",
			ParentIDColumn: "run_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "entry_type", Type: "text"},
				{Name: "direction", Type: "text"},
				{Name: "amount", Type: "numeric(14,2)"},
				{Name: "currency", Type: "char(3)"},
			},
			CheckConstraints: []string{
				"direction IN ('DEBIT', 'CREDIT')",
				"currency ~ '^[A-Z]{3}$'",
			},
			Notes: "Preview of PayrollLedgerEntry: the immutable per-run debit/credit trail a PayrollRun accrues (rewards-payroll-workforce.md ## Payroll).",
		},
	}
}

func benefitsDomain() Domain {
	return Domain{
		Number:            2,
		Slug:              "benefits",
		Title:             "Benefits",
		SourceDoc:         "planning/data/models/rewards-payroll-workforce.md",
		SourceSection:     "## Benefits",
		Disposition:       "DRAFT",
		DispositionReason: notFunded,
		Head: TableSpec{
			Table:          "benefit_election",
			IDColumn:       "election_id",
			KeyColumn:      "election_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "worker_ref", Type: "text"},
				{Name: "plan_ref", Type: "text"},
				{Name: "coverage_level", Type: "text"},
				{Name: "status", Type: "text", Default: "'ACTIVE'"},
			},
			CheckConstraints: []string{
				"status IN ('ACTIVE', 'WAIVED', 'PENDING', 'TERMINATED')",
			},
			Notes: "Preview of BenefitElection (rewards-payroll-workforce.md ## Benefits): a worker's current plan/coverage-level election.",
		},
		Evidence: TableSpec{
			Table:          "benefit_election_revision",
			IDColumn:       "revision_id",
			ParentTable:    "benefit_election",
			ParentIDColumn: "election_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "premium_amount", Type: "numeric(14,2)"},
				{Name: "currency", Type: "char(3)"},
				{Name: "covered_dependent_count", Type: "integer", Default: "0"},
			},
			CheckConstraints: []string{
				"currency ~ '^[A-Z]{3}$'",
				"covered_dependent_count >= 0",
			},
			Notes: "Preview of BenefitElectionRevision: the immutable premium/dependent snapshot each election change produces (rewards-payroll-workforce.md ## Benefits).",
		},
	}
}

func timeDomain() Domain {
	return Domain{
		Number:            3,
		Slug:              "time",
		Title:             "Time",
		SourceDoc:         "planning/data/models/rewards-payroll-workforce.md",
		SourceSection:     "## Time, attendance and scheduling",
		Disposition:       "DRAFT",
		DispositionReason: notFunded,
		Head: TableSpec{
			Table:          "timecard",
			IDColumn:       "timecard_id",
			KeyColumn:      "timecard_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "worker_ref", Type: "text"},
				{Name: "period_ref", Type: "text"},
				{Name: "status", Type: "text", Default: "'OPEN'"},
			},
			CheckConstraints: []string{
				"status IN ('OPEN', 'SUBMITTED', 'APPROVED', 'LOCKED')",
			},
			Notes: "Preview of Timecard (rewards-payroll-workforce.md ## Time, attendance and scheduling): one row per worker/period timecard and its submission state.",
		},
		Evidence: TableSpec{
			Table:          "timecard_revision",
			IDColumn:       "revision_id",
			ParentTable:    "timecard",
			ParentIDColumn: "timecard_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "total_regular_hours", Type: "numeric(8,2)", Default: "0"},
				{Name: "total_overtime_hours", Type: "numeric(8,2)", Default: "0"},
				{Name: "lock_state", Type: "text", Default: "'UNLOCKED'"},
			},
			CheckConstraints: []string{
				"total_regular_hours >= 0",
				"total_overtime_hours >= 0",
			},
			Notes: "Preview of TimecardRevision: the immutable calculated-totals snapshot a payroll cutoff binds to (rewards-payroll-workforce.md ## Time, attendance and scheduling).",
		},
	}
}

func leaveDomain() Domain {
	return Domain{
		Number:            4,
		Slug:              "leave",
		Title:             "Leave",
		SourceDoc:         "planning/data/models/rewards-payroll-workforce.md",
		SourceSection:     "## Leave, absence and accommodation",
		Disposition:       "CONFORMANCE",
		DispositionReason: "DB-023 already funds a dependency-tracked 'materialize the Leave domain state' todo (Depends: DB-004, DB-005, DB-007, LEAVE-002, BAL-001, MODEL-025), so this preview is proven against a named future authority gate rather than pure exploratory vocabulary.",
		Head: TableSpec{
			Table:          "leave_request_preview",
			IDColumn:       "request_id",
			KeyColumn:      "request_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "worker_ref", Type: "text"},
				{Name: "program_ref", Type: "text"},
				{Name: "status", Type: "text", Default: "'SUBMITTED'"},
			},
			CheckConstraints: []string{
				"status IN ('SUBMITTED', 'APPROVED', 'DENIED', 'ACTIVE', 'RETURNED', 'CANCELLED')",
			},
			Notes: "Preview of LeaveRequest (rewards-payroll-workforce.md ## Leave, absence and accommodation; DB-023): one row per worker leave request and its current disposition.",
		},
		Evidence: TableSpec{
			Table:          "leave_record_preview",
			IDColumn:       "record_id",
			ParentTable:    "leave_request_preview",
			ParentIDColumn: "request_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "leave_type", Type: "text"},
				{Name: "requested_hours", Type: "numeric(8,2)"},
			},
			CheckConstraints: []string{
				"requested_hours > 0",
			},
			Notes: "Preview of LeaveRecord: the immutable revision trail DB-023's GREEN clause requires for LeaveRequest/LeaveRecord (rewards-payroll-workforce.md ## Leave, absence and accommodation).",
		},
	}
}

func recruitingDomain() Domain {
	return Domain{
		Number:            5,
		Slug:              "recruiting",
		Title:             "Recruiting",
		SourceDoc:         "planning/data/models/talent-experience-cases.md",
		SourceSection:     "## Recruiting",
		Disposition:       "DRAFT",
		DispositionReason: notFunded,
		Head: TableSpec{
			Table:          "requisition",
			IDColumn:       "requisition_id",
			KeyColumn:      "requisition_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "position_ref", Type: "text"},
				{Name: "hiring_manager_ref", Type: "text"},
				{Name: "openings", Type: "integer", Default: "1"},
				{Name: "status", Type: "text", Default: "'DRAFT'"},
			},
			CheckConstraints: []string{
				"openings > 0",
				"status IN ('DRAFT', 'APPROVED', 'PUBLISHED', 'FILLED', 'CLOSED')",
			},
			Notes: "Preview of Requisition (talent-experience-cases.md ## Recruiting): a headcount request's position/hiring-manager/openings and lifecycle status.",
		},
		Evidence: TableSpec{
			Table:          "requisition_revision",
			IDColumn:       "revision_id",
			ParentTable:    "requisition",
			ParentIDColumn: "requisition_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "approved_openings", Type: "integer"},
				{Name: "approved_fte", Type: "numeric(6,4)"},
				{Name: "approval_digest", Type: "text"},
			},
			CheckConstraints: []string{
				"approved_openings > 0",
				"approved_fte > 0",
			},
			Notes: "Preview of RequisitionRevision: the immutable approval-certificate snapshot each requisition change produces (talent-experience-cases.md ## Recruiting).",
		},
	}
}

func talentDomain() Domain {
	return Domain{
		Number:            6,
		Slug:              "talent",
		Title:             "Talent",
		SourceDoc:         "planning/data/models/talent-experience-cases.md",
		SourceSection:     "## Performance and talent",
		Disposition:       "DRAFT",
		DispositionReason: notFunded,
		Head: TableSpec{
			Table:          "performance_review_preview",
			IDColumn:       "review_id",
			KeyColumn:      "review_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "cycle_ref", Type: "text"},
				{Name: "worker_ref", Type: "text"},
				{Name: "reviewer_ref", Type: "text"},
				{Name: "status", Type: "text", Default: "'IN_PROGRESS'"},
			},
			CheckConstraints: []string{
				"status IN ('IN_PROGRESS', 'SUBMITTED', 'ACKNOWLEDGED', 'APPEALED')",
			},
			Notes: "Preview of PerformanceReview (talent-experience-cases.md ## Performance and talent): a review cycle/worker/reviewer pairing and its submission status.",
		},
		Evidence: TableSpec{
			Table:          "performance_review_revision",
			IDColumn:       "revision_id",
			ParentTable:    "performance_review_preview",
			ParentIDColumn: "review_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "rating_value", Type: "numeric(4,2)"},
				{Name: "calibration_state", Type: "text", Default: "'PRE_CALIBRATION'"},
			},
			CheckConstraints: []string{
				"rating_value >= 0",
			},
			Notes: "Preview of PerformanceReviewRevision: the immutable rating/calibration snapshot each locked review revision produces (talent-experience-cases.md ## Performance and talent).",
		},
	}
}

func learningDomain() Domain {
	return Domain{
		Number:            7,
		Slug:              "learning",
		Title:             "Learning",
		SourceDoc:         "planning/data/models/talent-experience-cases.md",
		SourceSection:     "## Learning, skills and credentials",
		Disposition:       "DRAFT",
		DispositionReason: notFunded,
		Head: TableSpec{
			Table:          "learning_enrollment",
			IDColumn:       "enrollment_id",
			KeyColumn:      "enrollment_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "learner_ref", Type: "text"},
				{Name: "offering_ref", Type: "text"},
				{Name: "status", Type: "text", Default: "'REGISTERED'"},
			},
			CheckConstraints: []string{
				"status IN ('REGISTERED', 'WAITLISTED', 'ATTENDED', 'CANCELLED')",
			},
			Notes: "Preview of LearningEnrollment (talent-experience-cases.md ## Learning, skills and credentials): a learner's registration state against a learning offering.",
		},
		Evidence: TableSpec{
			Table:          "learning_completion",
			IDColumn:       "completion_id",
			ParentTable:    "learning_enrollment",
			ParentIDColumn: "enrollment_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "score", Type: "numeric(6,2)"},
				{Name: "result", Type: "text"},
				{Name: "verification_ref", Type: "text"},
			},
			CheckConstraints: []string{
				"result IN ('PASS', 'FAIL', 'INCOMPLETE', 'WAIVED')",
			},
			Notes: "Preview of LearningCompletion: the immutable score/result/verification evidence an enrollment's completion produces (talent-experience-cases.md ## Learning, skills and credentials).",
		},
	}
}

func caseDomain() Domain {
	return Domain{
		Number:            8,
		Slug:              "case",
		Title:             "Case",
		SourceDoc:         "planning/data/models/talent-experience-cases.md",
		SourceSection:     "## HR service, cases and employee relations",
		Disposition:       "DRAFT",
		DispositionReason: notFunded,
		Head: TableSpec{
			Table:          "hr_case",
			IDColumn:       "case_id",
			KeyColumn:      "case_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "case_type_ref", Type: "text"},
				{Name: "owner_ref", Type: "text"},
				{Name: "status", Type: "text", Default: "'OPEN'"},
			},
			CheckConstraints: []string{
				"status IN ('OPEN', 'IN_PROGRESS', 'ESCALATED', 'CLOSED', 'REOPENED')",
			},
			Notes: "Preview of Case (talent-experience-cases.md ## HR service, cases and employee relations): an HR case's type/owner and current disposition.",
		},
		Evidence: TableSpec{
			Table:          "case_transition",
			IDColumn:       "transition_id",
			ParentTable:    "hr_case",
			ParentIDColumn: "case_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "from_state", Type: "text"},
				{Name: "to_state", Type: "text"},
				{Name: "reason", Type: "text"},
			},
			CheckConstraints: nil,
			Notes:            "Preview of CaseTransition: the immutable from/to state-change trail a case's disposition history requires (talent-experience-cases.md ## HR service, cases and employee relations).",
		},
	}
}

func accessDomain() Domain {
	return Domain{
		Number:            9,
		Slug:              "access",
		Title:             "Access",
		SourceDoc:         "planning/data/models/connectivity-access-content.md",
		SourceSection:     "## Workforce identity and access",
		Disposition:       "DRAFT",
		DispositionReason: notFunded,
		Head: TableSpec{
			Table:          "access_grant",
			IDColumn:       "grant_id",
			KeyColumn:      "grant_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "principal_ref", Type: "text"},
				{Name: "entitlement_ref", Type: "text"},
				{Name: "source", Type: "text"},
				{Name: "status", Type: "text", Default: "'PENDING'"},
			},
			CheckConstraints: []string{
				"source IN ('BIRTHRIGHT', 'REQUEST', 'DELEGATION', 'EMERGENCY', 'MANUAL')",
				"status IN ('PENDING', 'GRANTED', 'REVOKED', 'EXPIRED')",
			},
			Notes: "Preview of AccessGrant (connectivity-access-content.md ## Workforce identity and access): a principal/entitlement grant and its current provisioning status.",
		},
		Evidence: TableSpec{
			Table:          "access_operation",
			IDColumn:       "operation_id",
			ParentTable:    "access_grant",
			ParentIDColumn: "grant_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "semantic_operation", Type: "text"},
				{Name: "expected_prior_state", Type: "text"},
				{Name: "new_state", Type: "text"},
			},
			CheckConstraints: nil,
			Notes:            "Preview of AccessOperation: the immutable executed-effect trail a grant's provisioning plan produces (connectivity-access-content.md ## Workforce identity and access).",
		},
	}
}

func regulatoryDomain() Domain {
	return Domain{
		Number:            10,
		Slug:              "regulatory",
		Title:             "Regulatory",
		SourceDoc:         "planning/data/models/rewards-payroll-workforce.md",
		SourceSection:     "## Tax and regulatory computation",
		Disposition:       "DRAFT",
		DispositionReason: notFunded,
		Head: TableSpec{
			Table:          "government_filing",
			IDColumn:       "filing_id",
			KeyColumn:      "filing_key",
			DataRole:       "AGGREGATE",
			RetentionClass: "OPERATIONAL",
			Columns: []Column{
				{Name: "authority_ref", Type: "text"},
				{Name: "report_definition_ref", Type: "text"},
				{Name: "period_ref", Type: "text"},
				{Name: "status", Type: "text", Default: "'PREPARED'"},
			},
			CheckConstraints: []string{
				"status IN ('PREPARED', 'SUBMITTED', 'ACCEPTED', 'REJECTED', 'UNKNOWN', 'AMENDED')",
			},
			Notes: "Preview of GovernmentFiling (rewards-payroll-workforce.md ## Tax and regulatory computation): a report-definition/authority/period filing and its submission status.",
		},
		Evidence: TableSpec{
			Table:          "filing_submission_attempt",
			IDColumn:       "attempt_id",
			ParentTable:    "government_filing",
			ParentIDColumn: "filing_id",
			AppendOnly:     true,
			DataRole:       "AGGREGATE",
			RetentionClass: "PERMANENT",
			Columns: []Column{
				{Name: "destination_ref", Type: "text"},
				{Name: "result", Type: "text"},
				{Name: "idempotency_key", Type: "semantic_key"},
			},
			CheckConstraints: nil,
			Notes:            "Preview of FilingSubmissionAttempt: the immutable idempotent submission trail a filing's transmission attempts require (rewards-payroll-workforce.md ## Tax and regulatory computation).",
		},
	}
}
