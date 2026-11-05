// WF-TEST-007: role-play approvals and tasks, and inject signals and
// vendor callbacks, inside a test run.
//
// Approvals, tasks and external callbacks block every realistic path, and
// a test run has no one to act on them. A test run therefore lets its
// author assume the routed approver or task owner through a scoped
// credential — the sandbox's scoped approver credential, bound to one run
// and one assumed identity — and inject correlated signals or vendor
// callbacks with chosen payloads. Everything is recorded in the run
// journal, which is test-only: the credential never validates outside its
// run, and nothing here authorizes production use.
package sandbox

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// Role-play errors. All are matchable with errors.Is.
var (
	// ErrRolePlayInvalid is returned for malformed role-play input.
	ErrRolePlayInvalid = errors.New("sandbox: invalid role-play request")
	// ErrForeignRun is returned when a credential is used in any run but
	// its own.
	ErrForeignRun = errors.New("sandbox: role-play credential belongs to another run")
	// ErrRunClosed is returned when a credential is used after its run
	// closed.
	ErrRunClosed = errors.New("sandbox: test run is closed")
	// ErrScopeDenied is returned when a credential lacks the scope for an
	// action.
	ErrScopeDenied = errors.New("sandbox: role-play credential lacks this scope")
	// ErrRoutedMismatch is returned when the assumed identity is not the
	// identity the work item was routed to.
	ErrRoutedMismatch = errors.New("sandbox: assumed identity is not the routed identity")
	// ErrTestOnly is returned for any attempt to use role-play outside a
	// test run. Role-play never grants authority outside the run it was
	// minted for.
	ErrTestOnly = errors.New("sandbox: role-play exists only inside a test run")
)

// RoleScope is one thing a role-play credential may do.
type RoleScope uint8

// Role scopes.
const (
	// ScopeUnspecified is the zero value and is never legal.
	ScopeUnspecified RoleScope = iota
	// ScopeApproval acts on routed approvals.
	ScopeApproval
	// ScopeTask completes routed tasks.
	ScopeTask
	// ScopeSignal injects correlated signals.
	ScopeSignal
	// ScopeCallback injects vendor callbacks.
	ScopeCallback
)

// TestRun is one open test run: the only place role-play exists. The
// journal records every acted approval, completed task and injected
// signal or callback, in order.
type TestRun struct {
	ID       string
	Author   string
	TestOnly bool
	open     bool
	journal  []JournalEntry
}

// OpenTestRun opens a test run for an author. Every run this function
// opens is test-only; there is no constructor for role-play in a
// production run.
func OpenTestRun(runID, author string) (*TestRun, error) {
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(author) == "" {
		return nil, fmt.Errorf("%w: run id and author are required", ErrRolePlayInvalid)
	}
	return &TestRun{ID: runID, Author: author, TestOnly: true, open: true}, nil
}

// Close ends the run. Every credential minted for it stops validating.
func (r *TestRun) Close() { r.open = false }

// Journal returns the run journal in order.
func (r *TestRun) Journal() []JournalEntry {
	return append([]JournalEntry(nil), r.journal...)
}

// RoleCredential is the sandbox's scoped approver credential: the author,
// acting as one routed identity, for a closed scope list, in one test run
// only.
type RoleCredential struct {
	RunID       string
	Author      string
	As          string
	Scopes      []RoleScope
	ApproverRef string
	TestOnly    bool
}

func (c RoleCredential) hasScope(s RoleScope) bool {
	for _, have := range c.Scopes {
		if have == s {
			return true
		}
	}
	return false
}

// AssumeRole mints a scoped credential for the author to act as the given
// routed identity. It works only while the run is open.
func (r *TestRun) AssumeRole(as string, scopes ...RoleScope) (RoleCredential, error) {
	if !r.open {
		return RoleCredential{}, fmt.Errorf("%w: %s", ErrRunClosed, r.ID)
	}
	if strings.TrimSpace(as) == "" {
		return RoleCredential{}, fmt.Errorf("%w: assumed identity is required", ErrRolePlayInvalid)
	}
	if len(scopes) == 0 {
		return RoleCredential{}, fmt.Errorf("%w: at least one scope is required", ErrRolePlayInvalid)
	}
	seen := make(map[RoleScope]bool, len(scopes))
	for _, s := range scopes {
		if s != ScopeApproval && s != ScopeTask && s != ScopeSignal && s != ScopeCallback {
			return RoleCredential{}, fmt.Errorf("%w: unknown scope %d", ErrRolePlayInvalid, s)
		}
		if seen[s] {
			return RoleCredential{}, fmt.Errorf("%w: duplicate scope %d", ErrRolePlayInvalid, s)
		}
		seen[s] = true
	}
	return RoleCredential{
		RunID:       r.ID,
		Author:      r.Author,
		As:          as,
		Scopes:      append([]RoleScope(nil), scopes...),
		ApproverRef: sandboxApproverRef,
		TestOnly:    true,
	}, nil
}

// AuthorizeProduction always refuses a role-play credential. This is the
// production gate: there is no input for which role-play authorizes
// production use.
func AuthorizeProduction(RoleCredential) error {
	return fmt.Errorf("%w: production use refused", ErrTestOnly)
}

func (r *TestRun) check(cred RoleCredential, scope RoleScope) error {
	if !cred.TestOnly {
		return fmt.Errorf("%w: credential marker stripped", ErrTestOnly)
	}
	if !r.open {
		return fmt.Errorf("%w: %s", ErrRunClosed, r.ID)
	}
	if cred.RunID != r.ID {
		return fmt.Errorf("%w: credential for %s used in %s", ErrForeignRun, cred.RunID, r.ID)
	}
	if !cred.hasScope(scope) {
		return fmt.Errorf("%w: scope %d", ErrScopeDenied, scope)
	}
	return nil
}

// ApprovalStep is one routed approval the author acts on.
type ApprovalStep struct {
	WorkItemID string
	RoutedTo   string
	Decision   workitem.ApprovalDecision
}

// ApproveAs records the approval decision as the routed approver.
func (r *TestRun) ApproveAs(cred RoleCredential, step ApprovalStep, at time.Time) error {
	if err := r.check(cred, ScopeApproval); err != nil {
		return err
	}
	if cred.As != step.RoutedTo {
		return fmt.Errorf("%w: acting as %s on work routed to %s", ErrRoutedMismatch, cred.As, step.RoutedTo)
	}
	if strings.TrimSpace(step.WorkItemID) == "" {
		return fmt.Errorf("%w: work item is required", ErrRolePlayInvalid)
	}
	if !step.Decision.Valid() {
		return fmt.Errorf("%w: decision %q", ErrRolePlayInvalid, step.Decision)
	}
	r.journal = append(r.journal, JournalEntry{
		At: at.UTC(), RunID: r.ID, Actor: cred.Author, As: cred.As,
		Kind: "approval", Ref: step.WorkItemID, Detail: string(step.Decision), TestOnly: true,
	})
	return nil
}

// TaskStep is one routed task the author completes.
type TaskStep struct {
	WorkItemID string
	RoutedTo   string
	OutputRef  string
}

// CompleteTaskAs records the task completion as the routed task owner.
func (r *TestRun) CompleteTaskAs(cred RoleCredential, step TaskStep, at time.Time) error {
	if err := r.check(cred, ScopeTask); err != nil {
		return err
	}
	if cred.As != step.RoutedTo {
		return fmt.Errorf("%w: acting as %s on work routed to %s", ErrRoutedMismatch, cred.As, step.RoutedTo)
	}
	if strings.TrimSpace(step.WorkItemID) == "" {
		return fmt.Errorf("%w: work item is required", ErrRolePlayInvalid)
	}
	r.journal = append(r.journal, JournalEntry{
		At: at.UTC(), RunID: r.ID, Actor: cred.Author, As: cred.As,
		Kind: "task", Ref: step.WorkItemID, Detail: step.OutputRef, TestOnly: true,
	})
	return nil
}

// SignalInjection is one correlated signal with a chosen payload.
type SignalInjection struct {
	CorrelationKey   string
	CorrelationValue string
	EventType        string
	Payload          []byte
	IdempotencyKey   string
}

// InjectedSignal is the recorded signal, safe to feed to signal
// correlation: it carries the key, value, payload and idempotency
// identity the injection chose.
type InjectedSignal struct {
	CorrelationKey   string
	CorrelationValue string
	EventType        string
	Payload          []byte
	IdempotencyKey   string
}

// InjectSignal injects a correlated signal into the test run.
func (r *TestRun) InjectSignal(cred RoleCredential, inj SignalInjection, at time.Time) (InjectedSignal, error) {
	if err := r.check(cred, ScopeSignal); err != nil {
		return InjectedSignal{}, err
	}
	if strings.TrimSpace(inj.CorrelationKey) == "" || strings.TrimSpace(inj.CorrelationValue) == "" || strings.TrimSpace(inj.EventType) == "" {
		return InjectedSignal{}, fmt.Errorf("%w: correlation key, value and event type are required", ErrRolePlayInvalid)
	}
	out := InjectedSignal{
		CorrelationKey:   inj.CorrelationKey,
		CorrelationValue: inj.CorrelationValue,
		EventType:        inj.EventType,
		Payload:          append([]byte(nil), inj.Payload...),
		IdempotencyKey:   inj.IdempotencyKey,
	}
	r.journal = append(r.journal, JournalEntry{
		At: at.UTC(), RunID: r.ID, Actor: cred.Author, As: cred.As,
		Kind: "signal", Ref: inj.CorrelationKey + "=" + inj.CorrelationValue, Detail: inj.EventType, TestOnly: true,
	})
	return out, nil
}

// CallbackInjection is one vendor callback with a chosen payload.
type CallbackInjection struct {
	Vendor     string
	CallbackID string
	Payload    []byte
}

// InjectedCallback is the recorded vendor callback.
type InjectedCallback struct {
	Vendor     string
	CallbackID string
	Payload    []byte
}

// InjectCallback injects a vendor callback into the test run.
func (r *TestRun) InjectCallback(cred RoleCredential, inj CallbackInjection, at time.Time) (InjectedCallback, error) {
	if err := r.check(cred, ScopeCallback); err != nil {
		return InjectedCallback{}, err
	}
	if strings.TrimSpace(inj.Vendor) == "" || strings.TrimSpace(inj.CallbackID) == "" {
		return InjectedCallback{}, fmt.Errorf("%w: vendor and callback id are required", ErrRolePlayInvalid)
	}
	out := InjectedCallback{
		Vendor:     inj.Vendor,
		CallbackID: inj.CallbackID,
		Payload:    append([]byte(nil), inj.Payload...),
	}
	r.journal = append(r.journal, JournalEntry{
		At: at.UTC(), RunID: r.ID, Actor: cred.Author, As: cred.As,
		Kind: "callback", Ref: inj.Vendor + ":" + inj.CallbackID, Detail: fmt.Sprintf("%d payload bytes", len(inj.Payload)), TestOnly: true,
	})
	return out, nil
}

// JournalEntry is one recorded role-play action. Every entry is test-only.
type JournalEntry struct {
	At       time.Time
	RunID    string
	Actor    string
	As       string
	Kind     string
	Ref      string
	Detail   string
	TestOnly bool
}
