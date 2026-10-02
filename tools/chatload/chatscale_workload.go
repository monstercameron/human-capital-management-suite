package chatload

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Mixed runs equal proportions of the eight operations. Each send commits the
// conversation counter, post, revision and outbox in one transaction. Many
// workers contend on one channel while an independent connection samples locks.
func Mixed(ctx context.Context, c Config, qs []Query) (Workload, error) {
	return mixed(ctx, c, qs, []string{"open_channel", "page_back", "send", "mark_read", "sidebar_300", "search", "thread_open", "outbox_drain"})
}
func mixed(ctx context.Context, c Config, qs []Query, names []string) (Workload, error) {
	if e := c.Validate(); e != nil {
		return Workload{}, e
	}
	queries := map[string]Query{}
	for _, name := range names {
		if name == "send" {
			continue
		}
		q, e := workloadQuery(qs, name)
		if e != nil {
			return Workload{}, e
		}
		if name == "open_channel" {
			head, e := queryBy(qs, "GetConversation", "SELECT")
			if e != nil {
				return Workload{}, e
			}
			q.headSQL = head.SQL
		}
		queries[name] = q
	}
	w := Workload{Ratio: fmt.Sprint(names) + " equal proportions", Concurrency: c.Concurrency}
	var mu sync.Mutex
	timings := map[string][]float64{}
	errs := map[string]int{}
	messages := map[string]string{}
	observer, e := pgx.ConnectConfig(ctx, databaseConfig(c))
	if e != nil {
		return w, e
	}
	defer observer.Close(ctx)
	stop := make(chan struct{})
	observed := make(chan struct{})
	go func() {
		defer close(observed)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var n int
				var age float64
				e := observer.QueryRow(ctx, `SELECT count(*),coalesce(max(extract(epoch FROM(now()-query_start))*1000),0) FROM pg_stat_activity WHERE datname=$1 AND wait_event_type='Lock'`, c.Database).Scan(&n, &age)
				if e == nil {
					w.LockSamples++
					if n > 0 {
						w.LockWaitSamples++
					}
					w.MaxLockWaiters = max(w.MaxLockWaiters, n)
					w.LockWaitAgeMS = max(w.LockWaitAgeMS, age)
				}
			}
		}
	}()
	start := time.Now()
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < c.Concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, e := pgx.ConnectConfig(ctx, databaseConfig(c))
			if e != nil {
				mu.Lock()
				messages["connection"] = e.Error()
				mu.Unlock()
				for range jobs {
				}
				return
			}
			defer conn.Close(ctx)
			bound := map[string][]any{}
			for name, q := range queries {
				a, e := arguments(ctx, conn, q, channelName(0))
				if e == nil {
					bound[name] = a
				}
			}
			for job := range jobs {
				name := names[job%len(names)]
				began := time.Now()
				err := runOperation(ctx, conn, c, name, queries[name], bound[name], job)
				duration := float64(time.Since(began).Microseconds()) / 1000
				mu.Lock()
				timings[name] = append(timings[name], duration)
				if err != nil {
					errs[name]++
					if messages[name] == "" {
						messages[name] = err.Error()
					}
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < c.Operations; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(stop)
	<-observed
	w.ElapsedSeconds = time.Since(start).Seconds()
	if messages["connection"] != "" {
		return w, fmt.Errorf("workload connection: %s", messages["connection"])
	}
	for _, name := range names {
		w.Operations = append(w.Operations, Operation{name, percentiles(timings[name]), errs[name], messages[name]})
	}
	w.SendsPerSecond = float64(len(timings["send"])-errs["send"]) / w.ElapsedSeconds
	return w, nil
}
func runOperation(ctx context.Context, conn *pgx.Conn, c Config, name string, q Query, args []any, job int) error {
	tx, e := conn.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, "SELECT set_config('hcmnext.tenant_id','t000',true),set_config('statement_timeout','15000',true)")
	if e != nil {
		return e
	}
	switch name {
	case "send":
		var member string
		if e = tx.QueryRow(ctx, `SELECT member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3 AND state='active' FOR SHARE`, "t000", channelName(0), "reader").Scan(&member); e != nil {
			return e
		}
		var route uint64
		if e = tx.QueryRow(ctx, `SELECT route_epoch FROM chat_conversation WHERE tenant_id=$1 AND id=$2 AND route_state='ACTIVE' FOR UPDATE`, "t000", channelName(0)).Scan(&route); e != nil {
			return e
		}
		var seq int64
		var event int64
		if e = tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1,post_sequence=post_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING event_sequence,post_sequence`, "t000", channelName(0)).Scan(&event, &seq); e != nil {
			return e
		}
		id := fmt.Sprintf("workload-%d-%d-%d", time.Now().UnixNano(), c.Posts, job)
		body := "Payroll review and onboarding workload send"
		if _, e = tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES($1,$2,$3,$4,$2,$5,$6)`, id, "t000", channelName(0), "reader", seq, body); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body) VALUES($1,$2,1,$3,$4)`, "t000", id, "reader", body); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'post.created',jsonb_build_object('ConversationID',$3::text,'TargetID',$2::text,'EventSequence',$4::bigint))`, "t000", id, channelName(0), event); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:t000"); e != nil {
			return e
		}
		var auditSeq int64
		if e = tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM chat_audit_event WHERE tenant_id=$1`, "t000").Scan(&auditSeq); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES($1,$2,$3,'reader','post.created','chat',$2,0,'post.created','fixture',now(),'fixture')`, "t000", id, auditSeq); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at) VALUES($1,$2,$3,'post',$2,1,now())`, "t000", id, channelName(0)); e != nil {
			return e
		}
	case "sidebar_300":
		for ch := 0; ch < 300; ch++ {
			a := append([]any(nil), args...)
			a[1] = channelName(ch)
			if e = queryTx(ctx, tx, q.SQL, a); e != nil {
				return e
			}
		}
	case "page_back":
		a := append([]any(nil), args...)
		a[4] = int64(100)
		e = queryTx(ctx, tx, q.SQL, a)
	case "mark_read":
		var maxSeq int64
		if e = tx.QueryRow(ctx, `SELECT max(sequence) FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2`, "t000", channelName(0)).Scan(&maxSeq); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence,revision) VALUES($1,$1,'reader',$2,$3,2) ON CONFLICT(tenant_id,home_tenant_id,member_id,conversation_id) DO UPDATE SET last_sequence=GREATEST(chat_cursor.last_sequence,EXCLUDED.last_sequence),revision=chat_cursor.revision+1,updated_at=now()`, "t000", channelName(0), maxSeq)
	case "outbox_drain":
		rows, err := tx.Query(ctx, q.SQL, args...)
		if err != nil {
			return err
		}
		ids := []int64{}
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, values[0].(int64))
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, id := range ids {
			if _, e = tx.Exec(ctx, `INSERT INTO chat_outbox_receipt(tenant_id,outbox_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, "t000", id); e != nil {
				return e
			}
		}
	default:
		if name == "open_channel" {
			if e = queryTx(ctx, tx, q.headSQL, []any{"t000", channelName(0)}); e != nil {
				return e
			}
		}
		e = queryTx(ctx, tx, q.SQL, args)
	}
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func queryTx(ctx context.Context, tx pgx.Tx, sql string, args []any) error {
	rows, e := tx.Query(ctx, sql, args...)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		if _, e = rows.Values(); e != nil {
			return e
		}
	}
	return rows.Err()
}
