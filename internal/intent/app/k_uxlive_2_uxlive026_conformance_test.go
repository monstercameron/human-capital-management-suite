package app

import (
	"strconv"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_UXLIVE_026_Conformance proves the server authority and the
// browser mirror agree for every wire stage, both started facts, and every
// stage-only intervention. BLOCKED is deliberately checked twice: it is the
// one stage whose answer changes with the run fact.
func TestTodo_UXLIVE_026_Conformance(t *testing.T) {
	var stages []workspace.JourneyStage
	for value, name := range journeyv1.JourneyStage_name {
		if value == 0 {
			continue
		}
		stages = append(stages, workspace.JourneyStage(strings.TrimPrefix(name, "JOURNEY_STAGE_")))
	}
	if len(stages) == 0 {
		t.Fatal("wire stage enum produced no conformance cases")
	}

	kinds := []struct {
		server workspace.JourneyInterventionKind
		client string
	}{
		{server: workspace.JourneyInterventionWithdraw, client: journeyclient.ActionWithdraw},
		{server: workspace.JourneyInterventionCancel, client: journeyclient.ActionCancel},
	}
	for _, kind := range kinds {
		for _, stage := range stages {
			for _, started := range []bool{false, true} {
				name := string(kind.server) + "/" + string(stage) + "/started=" + strconv.FormatBool(started)
				t.Run(name, func(t *testing.T) {
					serverReason, serverUnavailable := interventionUnavailableAtStage(kind.server, stage, started)
					clientReason, clientAvailable := journeyclient.InterventionAvailability(kind.client, string(stage), started)
					if (!serverUnavailable) != clientAvailable {
						t.Fatalf("server available=%v, client available=%v", !serverUnavailable, clientAvailable)
					}
					if serverReason != clientReason {
						t.Fatalf("server reason=%q, client reason=%q", serverReason, clientReason)
					}
				})
			}
		}
	}
}
