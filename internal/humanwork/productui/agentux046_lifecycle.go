package productui

import "strings"

// agentUX046EvaluationRunner is the colleague named when the reader may not
// run an evaluation: someone the directory lists as administering agents, who
// can therefore open Agent setup and run it. The reader is never named to
// themselves. The zero value means the directory lists nobody suitable.
func agentUX046EvaluationRunner(snapshot PersonaAdminSnapshot) PersonaAdminTarget {
	viewer := strings.TrimSpace(snapshot.ViewerSubject)
	for _, role := range []string{"agent_administrator", "hcm_admin"} {
		for _, option := range snapshot.SubjectOptions {
			candidate := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(option.Role)))
			if !strings.Contains(candidate, role) || strings.TrimSpace(option.Label) == "" {
				continue
			}
			if viewer != "" && strings.TrimSpace(option.ID) == viewer {
				continue
			}
			return option
		}
	}
	return PersonaAdminTarget{}
}
