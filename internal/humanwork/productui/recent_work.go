package productui

// RecentPerson is the compact person projection used by Home's continuity
// rail. It intentionally carries no sensitive fields or workflow authority.
type RecentPerson struct {
	ID       string
	Name     string
	Initials string
	PhotoURL string
	Role     string
	Href     string
	Navigate func(string)
}

// RecentPeople resolves the people referenced by a recent-work stream. Work
// order is the server-provided recency order; the helper only joins against
// the already authorized people projection and de-duplicates references.
// Unknown references are dropped fail-closed and the result is bounded.
func RecentPeople(people []Person, recent []WorkItem, limits ...int) []RecentPerson {
	limit := 5
	if len(limits) > 0 {
		limit = limits[0]
	}
	if limit <= 0 {
		return nil
	}
	byID := make(map[string]Person, len(people))
	byName := make(map[string]Person, len(people))
	for _, person := range people {
		if person.ID != "" {
			byID[person.ID] = person
		}
		if person.Name != "" {
			if previous, exists := byName[person.Name]; exists && previous.ID != person.ID {
				delete(byName, person.Name)
			} else {
				byName[person.Name] = person
			}
		}
	}
	seen := make(map[string]bool, limit)
	result := make([]RecentPerson, 0, limit)
	for _, item := range recent {
		person, ok := byID[item.PersonRef]
		if !ok && item.PersonRef != "" {
			person, ok = byID[stablePersonID(people, item.PersonRef)]
		}
		if !ok && item.Person != "" {
			person, ok = byName[item.Person]
		}
		if !ok || person.ID == "" || seen[person.ID] {
			continue
		}
		seen[person.ID] = true
		result = append(result, RecentPerson{ID: person.ID, Name: person.Name, Initials: person.Initials, PhotoURL: person.PhotoURL, Role: person.Role})
		if len(result) == limit {
			break
		}
	}
	return result
}

// RecentWork filters one admitted work stream to the items the
// recent-work slot governs: terminal items in admission order.
// Attention (WEB-098) covers the active side; terminal items
// belong to continuity and history. Order stays server-ranked;
// the compiler presents without assigning recency. The output
// never aliases the input.
func RecentWork(items []WorkItem) []WorkItem {
	recent := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if item.Terminal {
			recent = append(recent, item)
		}
	}
	return recent
}
