package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/experience/disposition"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/participants"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/userflow"
)

// ExperienceContracts is the serve-cell composition of the machine-readable
// user-flow vocabulary, accountable specification owners, participant
// resolver, and BusinessIntent disposition index.
type ExperienceContracts struct {
	Stages       []userflow.Stage
	Owners       *participants.SpecificationOwnerRegistry
	Resolver     participants.Resolver
	Dispositions *disposition.Registry
}

// NewExperienceContracts compiles the experience contracts from their
// canonical registries. The returned value is request-independent and carries
// no authorization state; stage resolution still consumes current governance.
func NewExperienceContracts() (*ExperienceContracts, error) {
	owners, err := participants.NewDefaultSpecificationOwnerRegistry()
	if err != nil {
		return nil, err
	}
	dispositions, err := disposition.NewDefault()
	if err != nil {
		return nil, err
	}
	return &ExperienceContracts{
		Stages:       userflow.Stages(),
		Owners:       owners,
		Resolver:     participants.Resolver{},
		Dispositions: dispositions,
	}, nil
}
