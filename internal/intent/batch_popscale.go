package intent

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/popscale"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
)

// popscaleSubjects serves one frozen snapshot's membership through a
// popscale session at the given page size. The batch compiler discloses
// membership (it names one child per member) but takes no count view, so a
// denied, empty or unknown count stays the one suppressed answer.
// Membership-protected snapshots never reach the session: CompileBatch
// refuses them with its explicit guard first. A snapshot whose recorded
// count contradicts its membership, or that counts the same subject twice,
// is refused outright instead of served.
func popscaleSubjects(snapshot population.Snapshot, pageSize int) ([]string, error) {
	// Frozen snapshots carry sorted membership, but hand-built snapshots may
	// not; the compiler keeps its long-standing tolerance by sorting its
	// working copy first, exactly as the direct read did. Duplicate members
	// stay refused: a snapshot that counts the same subject twice is
	// inconsistent and must not compile.
	ordered := snapshot.SubjectIDList()
	sort.Strings(ordered)
	session, err := popscale.NewSession(population.Snapshot{
		DefinitionID:     snapshot.DefinitionID,
		DefinitionDigest: snapshot.DefinitionDigest,
		RevisionVersion:  snapshot.RevisionVersion,
		SubjectIDs:       ordered,
		Count:            snapshot.Count,
		Digest:           snapshot.Digest,
		// Carried so the session answers a protected snapshot with the
		// zero page even if CompileBatch's explicit guard is ever moved.
		MembershipProtected: snapshot.MembershipProtected,
	}, popscale.Caller{MembershipDisclosed: true}, pageSize)
	if err != nil {
		return nil, fmt.Errorf("intent: batch population session: %w", err)
	}
	var subjects []string
	if err := session.Walk(func(page popscale.Page) error {
		subjects = append(subjects, page.Subjects...)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("intent: batch population pages: %w", err)
	}
	return subjects, nil
}
