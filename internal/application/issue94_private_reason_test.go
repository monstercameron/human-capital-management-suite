package application

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

// TestIssue94_PrivateReasonPrecedence pins which reason a private answer
// carries: the channel's requirement outranks the agent's own setting, and the
// agent's setting names itself only when the channel is not private.
func TestIssue94_PrivateReasonPrecedence(t *testing.T) {
	baseVersion, profile := authorityProfile(t)
	identity := agentsecurity.FinalOutputIdentity{TenantID: baseVersion.TenantID.String(), PersonaID: baseVersion.PersonaID, PersonaVersion: "2", InstallationID: "install-a", ConversationID: "room-a"}
	sealWith := func(alwaysPrivate bool) agentpersonastore.PersonaVersion {
		p := profile
		p.AlwaysPrivate = alwaysPrivate
		sealed, err := agentpersona.Seal(p)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		v := baseVersion
		v.Profile, v.ContentDigest = raw, sealed.Digest
		return v
	}
	install := func(channelPrivate bool) agentpersonastore.ActiveInstallation {
		policy := agentux037Accepted()
		policy.AlwaysPrivate = channelPrivate
		return agentpersonastore.ActiveInstallation{InstallationID: "install-a", PersonaID: "persona-a", PersonaVersion: 2, ConversationID: "room-a", ChannelPolicy: policy}
	}
	cases := []struct {
		name           string
		agentPrivate   bool
		channelPrivate bool
		want           string
		public         bool
	}{
		{name: "neither is private", public: true},
		{name: "channel private alone", channelPrivate: true, want: chat.PrivateReasonChannel},
		{name: "agent private alone", agentPrivate: true, want: chat.PrivateReasonAgent},
		{name: "both private names the channel", agentPrivate: true, channelPrivate: true, want: chat.PrivateReasonChannel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := personaRuntimePublicProfile(sealWith(tc.agentPrivate), install(tc.channelPrivate), identity)
			if tc.public {
				if err != nil {
					t.Fatalf("a public answer was refused: %v", err)
				}
				return
			}
			var private personaPrivateReasonError
			if !errors.As(err, &private) || private.reason != tc.want {
				t.Fatalf("reason = %v, want private with %v", err, tc.want)
			}
		})
	}
}
