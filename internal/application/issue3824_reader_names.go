package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// ChatgateReaderNames names the people who have read an applicant's answers.
// The ids come from the applicant's own list of reads, never from a request,
// so it discloses only who already read their answers (issue 3824).
type ChatgateReaderNames interface {
	GateReaderNames(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope, ids []string) (map[string]string, error)
}

// GateReaderNames is the full name of each requested reader the directory holds.
func (a *ChatgateAuthority) GateReaderNames(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope, ids []string) (map[string]string, error) {
	if err := a.discovers(ctx, actor, scope); err != nil && !a.administers(ctx, actor, scope) {
		return nil, err
	}
	people, err := a.people(ctx, scope.Tenant)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	names := map[string]string{}
	for _, person := range people {
		if wanted[person.Subject] && person.Name != "" {
			names[person.Subject] = person.Name
		}
	}
	return names, nil
}

var _ ChatgateReaderNames = (*ChatgateAuthority)(nil)

// nameGateReaders adds the names of the people who read the applicant's
// answers to the view, so the list says "Dana Whitfield", not "Administrators".
// A name that cannot be read is left out: the list then says "Administrators".
func (a *ChatgateApplication) nameGateReaders(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope, v *chatui.GateView) {
	directory, ok := a.Directory.(ChatgateReaderNames)
	if !ok {
		return
	}
	ids := []string{}
	for _, read := range v.Reads {
		if read.Reader != "" && read.Reader != actor.Person {
			ids = append(ids, read.Reader)
		}
	}
	if len(ids) == 0 {
		return
	}
	names, err := directory.GateReaderNames(ctx, actor, scope, ids)
	if err != nil {
		return
	}
	if v.Names == nil {
		v.Names = map[string]string{}
	}
	for id, name := range names {
		v.Names[id] = name
	}
}
