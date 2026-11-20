package agentcost

// Remaining is how much of today's allowance an agent has left in a
// conversation, as a whole percent from 0 to 100: the tightest of its runs and
// money limits, the agent's and the conversation's. It is false when no limit
// applies or the limits could not be read, so a page says nothing instead of
// guessing. It carries no actor: the figure is an aggregate and names no one.
func (g *Gate) Remaining(subject Subject) (int, bool) {
	day, _ := g.dayOf(g.now())
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.hydrateLocked(subject.TenantID); err != nil {
		return 0, false
	}
	left, known := 100, false
	for _, key := range keysFor(subject) {
		limit, ok := g.limits[key]
		if !ok {
			continue
		}
		var runs, micros int64
		if used := g.used[dayKey{key, day}]; used != nil {
			runs, micros = used.runs, used.micros
		}
		if limit.MaxRuns > 0 {
			known, left = true, min(left, remainingPercent(runs, limit.MaxRuns))
		}
		if limit.MaxSpendMicros > 0 {
			known, left = true, min(left, remainingPercent(micros, limit.MaxSpendMicros))
		}
	}
	return left, known
}

func remainingPercent(used, ceiling int64) int {
	if used >= ceiling {
		return 0
	}
	return int((ceiling - used) * 100 / ceiling)
}
