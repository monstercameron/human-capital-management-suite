package promotion

// Redrive re-runs validation and simulation for a stored package and records
// the fresh evidence on the record without moving its status. It is the
// operator "run it again" path: a redrive after a flake, a clock correction
// or a dependency change reproduces the governed evaluation with new
// timestamps rather than resurrecting stale evidence. Only records that
// already passed evaluation (simulated, approved or active) may be
// redriven; a draft or merely validated record has nothing to re-drive.
func (r *Registry) Redrive(id string) (Simulation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[id]
	if !ok {
		return Simulation{}, ErrNotFound
	}
	switch rec.Status {
	case StatusSimulated, StatusApproved, StatusActive:
	default:
		return Simulation{}, ErrInvalidTransition
	}
	v, err := validatePackage(clonePackage(rec.Package))
	if err != nil {
		return Simulation{}, err
	}
	s, err := Simulate(rec.Package, v)
	if err != nil {
		return Simulation{}, err
	}
	rec.Validation, rec.Simulation = v, s
	r.records[id] = rec
	return s, nil
}
