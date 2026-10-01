package chatrecipient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

var errAudienceDenied = errors.New("recipient cannot read disclosure")

type audienceFloorAuthority struct {
	snapshot         AudienceSnapshot
	snapshotErr      error
	denied           map[string]bool
	deniedDisclosure map[string]bool
	classDenied      map[dlp.DataClass]bool
	snapshotCalls    int
	authorizeCalls   int
	classCalls       int
	mu               sync.Mutex
}

func (a *audienceFloorAuthority) CurrentAudience(_ context.Context, _ chat.Conversation) (AudienceSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.snapshotCalls++
	return a.snapshot, a.snapshotErr
}

func (a *audienceFloorAuthority) AuthorizeDisclosure(_ context.Context, principal AudiencePrincipal, disclosure Disclosure) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.authorizeCalls++
	if a.denied[principal.TenantID+"/"+principal.SubjectID+"/"+disclosure.Field] || a.deniedDisclosure[disclosureKey(disclosure)] {
		return errAudienceDenied
	}
	return nil
}

func disclosureKey(disclosure Disclosure) string {
	return disclosure.SourceID + "\x00" + disclosure.RecordID + "\x00" + disclosure.Field + "\x00" + disclosure.CitationTitle
}

func (a *audienceFloorAuthority) AllowDataClass(_ context.Context, _ chat.Conversation, class dlp.DataClass) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.classCalls++
	if a.classDenied[class] {
		return errAudienceDenied
	}
	return nil
}

func floorConversation() chat.Conversation {
	return chat.Conversation{ID: "room", TenantID: "host", Kind: chat.PublicChannel, Revision: 7}
}

func floorDisclosure() Disclosure {
	return Disclosure{SourceID: "report", RecordID: "employee-1", Field: "compensation.base", CitationTitle: "Pay report", DataClass: dlp.ClassCompensation}
}

func floorAuthority() *audienceFloorAuthority {
	return &audienceFloorAuthority{snapshot: AudienceSnapshot{
		TenantID: "host", ConversationID: "room", Revision: 41,
		CurrentMembers:        []AudiencePrincipal{{TenantID: "host", SubjectID: "manager"}},
		EligibilityPopulation: []AudiencePrincipal{{TenantID: "host", SubjectID: "future-joiner"}, {TenantID: "guest-co", SubjectID: "guest", Guest: true, External: true}},
		Complete:              true, EligibilityComplete: true, GuestAndExternalComplete: true,
	}}
}

func TestTodo_AGENTP_012(t *testing.T) {
	authority := floorAuthority()
	got := EvaluateAudienceFloor(context.Background(), AudienceFloorRequest{Conversation: floorConversation(), Disclosures: []Disclosure{floorDisclosure()}}, authority)
	if got.Route != RoutePublic || got.Reason != AudienceFloorAllowed || got.SnapshotRevision != 41 || got.AudienceSize != 3 {
		t.Fatalf("decision = %+v", got)
	}
	if authority.snapshotCalls != 1 || authority.authorizeCalls != 3 || authority.classCalls != 1 {
		t.Fatalf("authority calls: snapshot=%d recipient=%d class=%d", authority.snapshotCalls, authority.authorizeCalls, authority.classCalls)
	}
}

func TestTodo_AGENTP_012_Golden(t *testing.T) {
	for _, tc := range []struct {
		name          string
		conversation  chat.Conversation
		alwaysPrivate bool
		wantRoute     DeliveryRoute
		wantReason    AudienceFloorReason
	}{
		{name: "public", conversation: floorConversation(), wantRoute: RoutePublic, wantReason: AudienceFloorAllowed},
		{name: "private channel", conversation: chat.Conversation{ID: "room", TenantID: "host", Kind: chat.PrivateChannel, Revision: 7}, wantRoute: RoutePrivate, wantReason: AudienceFloorPrivateChannel},
		{name: "direct", conversation: chat.Conversation{ID: "dm", TenantID: "host", Kind: chat.Direct, Revision: 7}, wantRoute: RoutePrivate, wantReason: AudienceFloorPrivateChannel},
		{name: "policy private", conversation: floorConversation(), alwaysPrivate: true, wantRoute: RoutePrivate, wantReason: AudienceFloorAlwaysPrivate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateAudienceFloor(context.Background(), AudienceFloorRequest{Conversation: tc.conversation, AlwaysPrivate: tc.alwaysPrivate, Disclosures: []Disclosure{floorDisclosure()}}, floorAuthority())
			if got.Route != tc.wantRoute || got.Reason != tc.wantReason {
				t.Fatalf("route/reason = %s/%s, want %s/%s", got.Route, got.Reason, tc.wantRoute, tc.wantReason)
			}
		})
	}
}

func TestTodo_AGENTP_012_Security(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*audienceFloorAuthority, *AudienceFloorRequest)
	}{
		{name: "denied future joiner", prepare: func(a *audienceFloorAuthority, _ *AudienceFloorRequest) {
			a.denied = map[string]bool{"host/future-joiner/compensation.base": true}
		}},
		{name: "denied external guest", prepare: func(a *audienceFloorAuthority, _ *AudienceFloorRequest) {
			a.denied = map[string]bool{"guest-co/guest/compensation.base": true}
		}},
		{name: "data class blocked", prepare: func(a *audienceFloorAuthority, _ *AudienceFloorRequest) {
			a.classDenied = map[dlp.DataClass]bool{dlp.ClassCompensation: true}
		}},
		{name: "citation title carries field disclosure", prepare: func(a *audienceFloorAuthority, r *AudienceFloorRequest) {
			r.Disclosures[0].CitationTitle = "Restricted employee salary"
			a.deniedDisclosure = map[string]bool{disclosureKey(r.Disclosures[0]): true}
		}},
		{name: "source and record are reauthorized", prepare: func(a *audienceFloorAuthority, r *AudienceFloorRequest) {
			r.Disclosures[0].SourceID = "private-document"
			r.Disclosures[0].RecordID = "private-record"
			a.deniedDisclosure = map[string]bool{disclosureKey(r.Disclosures[0]): true}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authority := floorAuthority()
			request := AudienceFloorRequest{Conversation: floorConversation(), Disclosures: []Disclosure{floorDisclosure()}}
			tc.prepare(authority, &request)
			got := EvaluateAudienceFloor(context.Background(), request, authority)
			if got.Route != RoutePrivate {
				t.Fatalf("unsafe disclosure routed %s", got.Route)
			}
		})
	}

	for _, tc := range []struct {
		name      string
		request   AudienceFloorRequest
		authority AudienceFloorAuthority
	}{
		{name: "nil authority", request: AudienceFloorRequest{Conversation: floorConversation(), Disclosures: []Disclosure{floorDisclosure()}}},
		{name: "no disclosures", request: AudienceFloorRequest{Conversation: floorConversation()}, authority: floorAuthority()},
		{name: "unknown data class", request: AudienceFloorRequest{Conversation: floorConversation(), Disclosures: []Disclosure{{SourceID: "s", RecordID: "r", Field: "f", DataClass: "UNKNOWN"}}}, authority: floorAuthority()},
		{name: "missing source", request: AudienceFloorRequest{Conversation: floorConversation(), Disclosures: []Disclosure{{RecordID: "r", Field: "f", DataClass: dlp.ClassPublic}}}, authority: floorAuthority()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateAudienceFloor(context.Background(), tc.request, tc.authority); got.Route != RoutePrivate {
				t.Fatalf("invalid request routed %s", got.Route)
			}
		})
	}
}

func TestTodo_AGENTP_012_Property(t *testing.T) {
	for memberCount := 1; memberCount <= 8; memberCount++ {
		for eligibleCount := 1; eligibleCount <= 6; eligibleCount++ {
			authority := floorAuthority()
			authority.snapshot.CurrentMembers = make([]AudiencePrincipal, memberCount)
			authority.snapshot.EligibilityPopulation = make([]AudiencePrincipal, eligibleCount)
			for i := range authority.snapshot.CurrentMembers {
				authority.snapshot.CurrentMembers[i] = AudiencePrincipal{TenantID: "host", SubjectID: fmt.Sprintf("member-%d", i)}
			}
			for i := range authority.snapshot.EligibilityPopulation {
				authority.snapshot.EligibilityPopulation[i] = AudiencePrincipal{TenantID: "host", SubjectID: fmt.Sprintf("eligible-%d", i)}
			}
			denied := ""
			if (memberCount+eligibleCount)%2 == 0 {
				denied = fmt.Sprintf("host/eligible-%d/compensation.base", eligibleCount/2)
				authority.denied = map[string]bool{denied: true}
			}
			got := EvaluateAudienceFloor(context.Background(), AudienceFloorRequest{Conversation: floorConversation(), Disclosures: []Disclosure{floorDisclosure()}}, authority)
			if denied != "" && got.Route != RoutePrivate {
				t.Fatalf("audience %d/%d contained unreadable fact %q but route=%s", memberCount, eligibleCount, denied, got.Route)
			}
			if denied == "" && got.Route != RoutePublic {
				t.Fatalf("fully authorized audience %d/%d routed privately: %+v", memberCount, eligibleCount, got)
			}
		}
	}
}

func TestTodo_AGENTP_012_Race(t *testing.T) {
	authority := floorAuthority()
	request := AudienceFloorRequest{Conversation: floorConversation(), Disclosures: []Disclosure{floorDisclosure()}}
	// A membership update committed before this evaluation is present in the
	// newly resolved snapshot and must be included before a public decision.
	authority.snapshot.EligibilityPopulation = append(authority.snapshot.EligibilityPopulation, AudiencePrincipal{TenantID: "guest-co", SubjectID: "joined-guest", Guest: true, External: true})
	authority.snapshot.Revision++
	authority.denied = map[string]bool{"guest-co/joined-guest/compensation.base": true}
	if got := EvaluateAudienceFloor(context.Background(), request, authority); got.Route != RoutePrivate || got.SnapshotRevision != 0 {
		t.Fatalf("new member was not honored: %+v", got)
	}

	// Concurrent requests each resolve the authority and never share cached
	// audience decisions across callers.
	const workers = 24
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := EvaluateAudienceFloor(context.Background(), request, authority); got.Route != RoutePrivate {
				errs <- fmt.Errorf("concurrent evaluation route=%s", got.Route)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if authority.snapshotCalls != workers+1 {
		t.Fatalf("snapshot calls=%d, want fresh resolution per evaluation", authority.snapshotCalls)
	}
}

func TestTodo_AGENTP_012_Mutation(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*AudienceSnapshot)
	}{
		{name: "current membership completeness", mutate: func(s *AudienceSnapshot) { s.Complete = false }},
		{name: "current audience removed", mutate: func(s *AudienceSnapshot) { s.CurrentMembers = nil }},
		{name: "public eligibility population", mutate: func(s *AudienceSnapshot) { s.EligibilityComplete = false }},
		{name: "guest and external coverage", mutate: func(s *AudienceSnapshot) { s.GuestAndExternalComplete = false }},
		{name: "future audience removed", mutate: func(s *AudienceSnapshot) { s.EligibilityPopulation = nil }},
		{name: "revision removed", mutate: func(s *AudienceSnapshot) { s.Revision = 0 }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			authority := floorAuthority()
			mutation.mutate(&authority.snapshot)
			got := EvaluateAudienceFloor(context.Background(), AudienceFloorRequest{Conversation: floorConversation(), Disclosures: []Disclosure{floorDisclosure()}}, authority)
			if got.Route != RoutePrivate {
				t.Fatalf("removed audience term still allowed public route: %+v", got)
			}
		})
	}
}
