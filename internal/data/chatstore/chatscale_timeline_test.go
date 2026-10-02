package chatstore

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATSCALE_002_Property_Timeline(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	const outbox = `SELECT g*2 AS id,'post.created' AS event_type,jsonb_build_object('tenant',$1::text,'conversation',$2::text,'post',g) AS payload,'2024-01-01'::timestamptz AS created_at FROM generate_series(1,1050) g WHERE g*2>$3`
	const ephemeral = `SELECT g*2-1 AS id,'ephemeral.post' AS event_type,CASE WHEN g%3=0 THEN '{}'::jsonb ELSE jsonb_build_object('post',g) END AS payload,'2024-01-01'::timestamptz AS created_at FROM generate_series(1,1050) g WHERE g*2-1>$3`
	for _, sources := range [][2]string{{outbox, ephemeral}, {outbox + " AND false", ephemeral}, {outbox, ephemeral + " AND false"}} {
		for _, edge := range []int64{0, 127, 1049, 2099, 2101} {
			for _, limit := range []int{1, 7, 50, 100} {
				if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
					read := func(sql string) ([]string, error) {
						rows, err := tx.Query(ctx, sql, chatscaleTenant, chatscaleRoom, edge, limit)
						if err != nil {
							return nil, err
						}
						defer rows.Close()
						var values []string
						for rows.Next() {
							var row string
							if err := rows.Scan(&row); err != nil {
								return nil, err
							}
							values = append(values, row)
						}
						return values, rows.Err()
					}
					oracle := `SELECT row_to_json(timeline)::text FROM (` + sources[0] + ` UNION ALL ` + sources[1] + `) AS timeline ORDER BY id LIMIT $4`
					bounded := strings.Replace(chatscaleTimelineSQL(sources[0], sources[1]), "SELECT id,event_type,payload,created_at FROM", "SELECT row_to_json(timeline)::text FROM", 1)
					want, err := read(oracle)
					if err != nil {
						return err
					}
					got, err := read(bounded)
					if err != nil {
						return err
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("offset=%d limit=%d bounded=%v oracle=%v", edge, limit, got, want)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
