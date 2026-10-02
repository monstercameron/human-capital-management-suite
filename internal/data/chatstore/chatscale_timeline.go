package chatstore

// Taking at most a page from each ordered source preserves the first page of
// their merge, including hidden ephemeral offsets. It avoids sorting every
// remaining outbox payload before returning a hundred events.
func chatscaleTimelineSQL(outbox, ephemeral string) string {
	return `SELECT id,event_type,payload,created_at FROM ((` + outbox + ` ORDER BY id LIMIT $4) UNION ALL (` + ephemeral + ` ORDER BY id LIMIT $4)) AS timeline ORDER BY id LIMIT $4`
}
