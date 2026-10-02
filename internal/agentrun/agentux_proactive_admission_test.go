package agentrun

import (
	"context"
	"testing"
	"time"
)

func TestAgentUXProactive_AnnouncementServiceActor_Security(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	base := validAdmissionRequest(now)
	base.Source = SourceIdentity{TenantID: "tenant-a", Kind: SourceAnnouncement, Key: "occurrence", Ref: "occurrence"}
	base.CauseID = "occurrence"
	base.Principal = PrincipalChain{Mode: ModeSponsored, AgentPrincipalID: "service-principal", SponsorID: "workload:policy", RequesterID: "owner"}
	for name, change := range map[string]func(*Request){
		"service actor":       func(*Request) {},
		"borrowed invoker":    func(r *Request) { r.Principal.InvokerID = "owner" },
		"borrowed credential": func(r *Request) { r.Principal.DelegatedCredentialRef = "owner-credential" },
		"missing owner":       func(r *Request) { r.Principal.RequesterID = "" },
		"wrong cause":         func(r *Request) { r.CauseID = "another-occurrence" },
		"missing persona":     func(r *Request) { r.Persona = nil },
	} {
		t.Run(name, func(t *testing.T) {
			request := base
			change(&request)
			authority := &admissionAuthority{snapshot: snapshotFor(request)}
			service, err := NewAdmissionService(AdmissionConfig{Authority: authority, Store: NewMemoryAdmissionStore(), Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			record, created, err := service.Admit(context.Background(), request)
			want := DecisionRefused
			if name == "service actor" {
				want = DecisionAccepted
			}
			if err != nil || !created || record.Decision != want {
				t.Fatalf("admission=%+v created=%t %v", record, created, err)
			}
			if want == DecisionAccepted && (record.Request.Principal.InvokerID != "" || record.Request.Principal.SponsorID == record.Request.Principal.RequesterID || record.Authority.Principal.RequesterID != "owner") {
				t.Fatal("owner became the acting authority")
			}
		})
	}
}
