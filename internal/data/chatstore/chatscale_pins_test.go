package chatstore

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

const chatscaleLegacyPinsSQL = `SELECT pin.conversation_id,pin.post_id,pin.tenant_id,pin.home_tenant_id,pin.member_id,pin.revision,pin.created_at FROM chat_pin pin JOIN chat_post post ON post.tenant_id=pin.tenant_id AND post.conversation_id=pin.conversation_id AND post.id=pin.post_id WHERE pin.tenant_id=$1 AND pin.conversation_id=$2 AND post.tombstoned=false ORDER BY pin.created_at DESC,pin.post_id DESC LIMIT 200`

const chatscaleScalarPinsSQL = `SELECT pin.conversation_id,pin.post_id,pin.tenant_id,pin.home_tenant_id,pin.member_id,pin.revision,pin.created_at FROM chat_pin pin WHERE pin.tenant_id=$1 AND pin.conversation_id=$2 AND (SELECT true FROM chat_post post WHERE post.tenant_id=$1 AND post.conversation_id=$2 AND post.id=pin.post_id AND NOT post.tombstoned LIMIT 1) IS TRUE ORDER BY pin.created_at DESC,pin.post_id DESC LIMIT 200`

const chatscaleLateralPinsSQL = `SELECT pin.conversation_id,pin.post_id,pin.tenant_id,pin.home_tenant_id,pin.member_id,pin.revision,pin.created_at FROM chat_pin pin JOIN LATERAL (SELECT true FROM chat_post post WHERE post.tenant_id=$1 AND post.conversation_id=$2 AND post.id=pin.post_id AND NOT post.tombstoned LIMIT 1) visible ON true WHERE pin.tenant_id=$1 AND pin.conversation_id=$2 ORDER BY pin.created_at DESC,pin.post_id DESC LIMIT 200`

type chatscalePinOracleRow struct {
	Conversation string    `json:"conversation_id"`
	Post         string    `json:"post_id"`
	Tenant       string    `json:"tenant_id"`
	Home         string    `json:"home_tenant_id"`
	Member       string    `json:"member_id"`
	Revision     uint64    `json:"revision"`
	Created      time.Time `json:"created_at"`
}

func TestTodo_CHATSCALE_002_Performance_Pins(t *testing.T) {
	s, resumed := chatscaleVolumeFixture(t)
	if !resumed {
		chatscaleSeed(t, s, chatscaleSize())
	}
	metrics := chatscaleMeasure(t, s, "pins-query", []chatscaleRead{
		{"join", chatscaleLegacyPinsSQL, []any{chatscaleTenant, chatscaleRoom}},
		{"scalar", chatscaleScalarPinsSQL, []any{chatscaleTenant, chatscaleRoom}},
		{"lateral", chatscaleLateralPinsSQL, []any{chatscaleTenant, chatscaleRoom}},
	})
	if *chatscaleReportPath != "" {
		raw, err := json.MarshalIndent(metrics, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(*chatscaleReportPath, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_CHATSCALE_002_Property_Pins(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 50000)
	ctx := context.Background()
	if err := s.execTenant(ctx, chatscaleTenant, `INSERT INTO chat_pin(tenant_id,home_tenant_id,conversation_id,post_id,member_id,created_at)
 SELECT tenant_id,tenant_id,conversation_id,id,$3,created_at FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND NOT tombstoned ORDER BY created_at DESC,id DESC LIMIT 400 ON CONFLICT DO NOTHING`, chatscaleTenant, chatscaleRoom, chatscaleReader); err != nil {
		t.Fatal(err)
	}
	for _, hide := range []int{0, 70, 250} {
		var oracle []chatscalePinOracleRow
		if err := s.execTenant(ctx, chatscaleTenant, `UPDATE chat_post SET tombstoned=true WHERE tenant_id=$1 AND conversation_id=$2 AND id IN(SELECT post_id FROM chat_pin WHERE tenant_id=$1 AND conversation_id=$2 ORDER BY created_at DESC,post_id DESC LIMIT $3)`, chatscaleTenant, chatscaleRoom, hide); err != nil {
			t.Fatal(err)
		}
		if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
			read := func(sql string) ([]string, error) {
				rows, err := tx.Query(ctx, "SELECT row_to_json(p)::text FROM ("+sql+") p", chatscaleTenant, chatscaleRoom)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				var out []string
				for rows.Next() {
					var row string
					if err := rows.Scan(&row); err != nil {
						return nil, err
					}
					out = append(out, row)
				}
				return out, rows.Err()
			}
			want, err := read(chatscaleLegacyPinsSQL)
			if err != nil {
				return err
			}
			if len(want) != 200 {
				t.Fatalf("fixture pins=%d want 200 after %d hidden", len(want), hide)
			}
			for _, row := range want {
				var v chatscalePinOracleRow
				if err := json.Unmarshal([]byte(row), &v); err != nil {
					return err
				}
				oracle = append(oracle, v)
			}
			for _, sql := range []string{chatscaleScalarPinsSQL, chatscaleLateralPinsSQL} {
				got, err := read(sql)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("hidden=%d pins differ from visibility/order oracle", hide)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		actual, err := NewAdapter(s).ListPins(ctx, chatscaleTenant, chatscaleRoom)
		if err != nil || len(actual) != len(oracle) {
			t.Fatalf("native pins=%d oracle=%d err=%v", len(actual), len(oracle), err)
		}
		for i, pin := range actual {
			v := oracle[i]
			if pin.ConversationID != v.Conversation || pin.PostID != v.Post || pin.TenantID != v.Tenant ||
				pin.PinnedByHomeTenantID != v.Home || pin.PinnedBy != v.Member || pin.Revision != v.Revision || !pin.CreatedAt.Equal(v.Created) {
				t.Fatalf("native pin %d=%+v oracle=%+v", i, pin, v)
			}
		}
	}
}
