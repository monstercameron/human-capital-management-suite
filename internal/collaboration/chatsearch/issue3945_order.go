package chatsearch

import (
	"sort"
	"time"
)

// Result order (ISSUE-3945). A search answers newest first unless the reader
// asks for the oldest first; the choice rides the request and is bound into the
// paging cursor, so a page can never be replayed in the other direction.
const (
	OrderNewest = "newest"
	OrderOldest = "oldest"
)

// Oldest reports whether the request asks for the oldest matches first.
func (q Request) Oldest() bool { return q.Order == OrderOldest }

// validOrder accepts the empty default and the two named orders.
func validOrder(order string) bool {
	return order == "" || order == OrderNewest || order == OrderOldest
}

// servedBefore reports whether a row lies at or before the cursor position in
// the requested order, which means it was already served on an earlier page.
func (q Request) servedBefore(row Row, at time.Time, key string) bool {
	if q.Oldest() {
		return row.At.Before(at) || row.At.Equal(at) && rowKey(row) <= key
	}
	return row.At.After(at) || row.At.Equal(at) && rowKey(row) >= key
}

// sortRows orders rows by (At, kind, id) in the requested direction. The kind
// and id tie-break is total, so the paging cursor never skips or repeats a row.
func (q Request) sortRows(rows []Row) {
	oldest := q.Oldest()
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].At.Equal(rows[j].At) {
			if oldest {
				return rowKey(rows[i]) < rowKey(rows[j])
			}
			return rowKey(rows[i]) > rowKey(rows[j])
		}
		if oldest {
			return rows[i].At.Before(rows[j].At)
		}
		return rows[i].At.After(rows[j].At)
	})
}
