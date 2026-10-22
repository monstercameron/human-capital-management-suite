package chatgate

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

// MemoryRepository is a deterministic transactional fixture and a fail-closed
// in-process adapter. Durable installations use the chat database repository.
type MemoryRepository struct {
	mu             sync.Mutex
	states         map[Scope]State
	answers        map[Scope][]Answer
	members        map[Scope]map[string]string
	FailMembership error
	// FailOutbox makes the commit of any change that raised events fail, as an
	// unavailable outbox does: nothing of the transaction is kept.
	FailOutbox error
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{states: map[Scope]State{}, answers: map[Scope][]Answer{}, members: map[Scope]map[string]string{}}
}

type memoryTransaction struct {
	state   State
	answers []Answer
	members map[string]string
	fail    error
}

func (t *memoryTransaction) Members(context.Context) ([]string, error) {
	out := make([]string, 0, len(t.members))
	for p := range t.members {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
func (t *memoryTransaction) IsMember(_ context.Context, person string) (bool, error) {
	return t.members[person] != "", nil
}
func (t *memoryTransaction) State() *State      { return &t.state }
func (t *memoryTransaction) Answers() *[]Answer { return &t.answers }
func (t *memoryTransaction) Membership(_ context.Context, a Actor, version string, admit bool) error {
	if t.fail != nil {
		return t.fail
	}
	if admit {
		t.members[a.Person] = version
	} else {
		delete(t.members, a.Person)
	}
	return nil
}
func (r *MemoryRepository) Transact(_ context.Context, scope Scope, fn func(Transaction) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	tx := &memoryTransaction{members: map[string]string{}, fail: r.FailMembership}
	b, e := json.Marshal(r.states[scope])
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &tx.state); e != nil {
		return e
	}
	b, e = json.Marshal(r.answers[scope])
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &tx.answers); e != nil {
		return e
	}
	for k, v := range r.members[scope] {
		tx.members[k] = v
	}
	before := len(tx.state.Events)
	if e := fn(tx); e != nil {
		return e
	}
	if r.FailOutbox != nil && len(tx.state.Events) > before {
		return r.FailOutbox
	}
	// Do not let a returned projection retain aliases into committed state.
	b, e = json.Marshal(tx.state)
	if e != nil {
		return e
	}
	var committed State
	if e = json.Unmarshal(b, &committed); e != nil {
		return e
	}
	r.states[scope] = committed
	b, e = json.Marshal(tx.answers)
	if e != nil {
		return e
	}
	var answers []Answer
	if e = json.Unmarshal(b, &answers); e != nil {
		return e
	}
	r.answers[scope] = answers
	r.members[scope] = tx.members
	return nil
}

// AddExisting records people who were in the channel before the gate was.
func (r *MemoryRepository) AddExisting(scope Scope, people ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.members[scope] == nil {
		r.members[scope] = map[string]string{}
	}
	for _, p := range people {
		r.members[scope][p] = "0.0.0"
	}
}

// ReadState hands over the committed state without the copy a change makes.
// The function must not keep or change it.
func (r *MemoryRepository) ReadState(_ context.Context, scope Scope, fn func(State) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(r.states[scope])
}
func (r *MemoryRepository) Member(scope Scope, person string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.members[scope][person] != ""
}
func (r *MemoryRepository) ListGateScopes(_ context.Context, tenant string) ([]Scope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Scope{}
	for scope, st := range r.states {
		if scope.Tenant == tenant && st.Gate.Current != "" {
			out = append(out, scope)
		}
	}
	return out, nil
}
