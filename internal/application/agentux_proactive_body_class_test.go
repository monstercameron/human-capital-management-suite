package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestAgentUXProactive_SponsoredBodyClass_Security(t *testing.T) {
	classifier, err := NewLocalDevPersonaPostClassifier(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	r := &AgentAnnouncementRuntime{BodyClasses: classifier}
	if class, err := r.bodyClass(context.Background(), "ironridge-demo", "Thanksgiving is November 26."); err != nil || class != dlp.ClassInternal {
		t.Fatalf("safe sponsored text=%s %v", class, err)
	}
	for _, test := range []struct{ tenant, text string }{{"another-tenant", "Thanksgiving"}, {"ironridge-demo", "My salary is $90000"}} {
		if _, err := r.bodyClass(context.Background(), test.tenant, test.text); err == nil {
			t.Fatalf("unsafe public output: %+v", test)
		}
	}
}
