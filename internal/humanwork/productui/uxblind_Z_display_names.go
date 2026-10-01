package productui

// workerDisplayNames applies the one preferred-plus-family projection to
// every admitted work item. Work and history rows carry only a worker
// reference, so this join lets older journey summaries inherit the same
// viewer-safe name as Organization without changing the authorized slice.
func workerDisplayNames(items []WorkItem, people []Person) []WorkItem {
	if len(items) == 0 || len(people) == 0 {
		return items
	}
	byRef := make(map[string]string, len(people)*2)
	for _, person := range people {
		name := PreferredFamilyName(person)
		if name == "" {
			continue
		}
		if person.ID != "" {
			byRef[person.ID] = name
		}
		if person.WorkerID != "" {
			byRef[person.WorkerID] = name
		}
	}
	if len(byRef) == 0 {
		return items
	}
	projected := append([]WorkItem(nil), items...)
	for index := range projected {
		if name := byRef[projected[index].PersonRef]; name != "" {
			projected[index].Person = name
		}
	}
	return projected
}
