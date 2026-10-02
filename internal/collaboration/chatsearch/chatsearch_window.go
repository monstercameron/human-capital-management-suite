package chatsearch

import "sort"

// WindowRows pages an already authorized, already matched source projection.
// It never ranks or counts a hidden row. Sources without a database keyset
// index can use this for their bounded directory or personal list.
func WindowRows(rows []Row, q Request) []Row {
	out := []Row{}
	for _, row := range rows {
		if q.BeforeKey != "" && (row.At.After(q.BeforeAt) || row.At.Equal(q.BeforeAt) && rowKey(row) >= q.BeforeKey) {
			continue
		}
		if q.OpenIDs != nil {
			found := false
			for _, id := range q.OpenIDs {
				found = found || row.ID == id
			}
			if !found {
				continue
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return rowKey(out[i]) > rowKey(out[j])
		}
		return out[i].At.After(out[j].At)
	})
	limit := q.Limit
	if limit == 0 {
		limit = 20
	}
	if len(out) > limit+1 {
		out = out[:limit+1]
	}
	return out
}
