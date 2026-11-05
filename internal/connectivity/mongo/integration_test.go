package mongo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/datasource"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestTodo_WF_DATA_015_Integration drives the real adapter code against
// the driver's own mock deployment: no daemon and no sockets, but real
// driver request/response round trips through Query, Write and Close.
func TestTodo_WF_DATA_015_Integration(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	ctx := context.Background()
	const ns = "ledgerdb.entries"

	mt.Run("query decodes cursor batches to canonical documents", func(mt *mtest.T) {
		mt.AddMockResponses(
			mtest.CreateCursorResponse(1, ns, mtest.FirstBatch,
				bson.D{{Key: "_id", Value: int32(1)}, {Key: "name", Value: "ann"}},
				bson.D{{Key: "_id", Value: int32(2)}, {Key: "name", Value: "bob"}},
			),
			mtest.CreateCursorResponse(0, ns, mtest.NextBatch),
		)
		conn := newConnector(mt.Client, testSource())
		docs, err := conn.Query(ctx, datasource.Query{Collection: "entries", Predicate: `{"name":"ann"}`})
		if err != nil {
			t.Fatalf("querying the mock deployment: %v", err)
		}
		if len(docs) != 2 || string(docs[0]) != `{"_id":1,"name":"ann"}` || string(docs[1]) != `{"_id":2,"name":"bob"}` {
			t.Fatalf("documents = %q, want the two canonical rows", docs)
		}
	})

	mt.Run("query surfaces server errors as unavailable", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{
			Code:    8,
			Message: "mock server exploded",
			Name:    "UnknownError",
		}))
		conn := newConnector(mt.Client, testSource())
		_, err := conn.Query(ctx, datasource.Query{Collection: "entries"})
		if !errors.Is(err, datasource.ErrUnavailable) {
			t.Fatalf("server error: %v, want ErrUnavailable", err)
		}
		if got := err.Error(); !strings.Contains(got, "tenant-a") || !strings.Contains(got, "ledger") || !strings.Contains(got, "entries") {
			t.Fatalf("server error %q names neither source nor collection", got)
		}
	})

	mt.Run("write inserts into a declared collection", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateSuccessResponse())
		src := testSource()
		src.ReadOnly = false
		src.WritableCollections = []string{"entries"}
		conn := newConnector(mt.Client, src)
		err := conn.Write(ctx, datasource.Write{
			Collection: "entries",
			Documents: []datasource.Document{
				datasource.Document(`{"_id":1,"name":"ann"}`),
				datasource.Document(`{"_id":{"$oid":"507f1f77bcf86cd799439011"}}`),
			},
		})
		if err != nil {
			t.Fatalf("writing to the mock deployment: %v", err)
		}
	})

	mt.Run("write refusals never reach the deployment", func(mt *mtest.T) {
		conn := newConnector(mt.Client, testSource())
		err := conn.Write(ctx, datasource.Write{Collection: "entries", Documents: []datasource.Document{[]byte(`{"a":1}`)}})
		if !errors.Is(err, datasource.ErrReadOnly) {
			t.Fatalf("read-only write: %v, want ErrReadOnly", err)
		}
		src := testSource()
		src.ReadOnly = false
		src.WritableCollections = []string{"entries"}
		conn = newConnector(mt.Client, src)
		err = conn.Write(ctx, datasource.Write{Collection: "audit", Documents: []datasource.Document{[]byte(`{"a":1}`)}})
		if !errors.Is(err, datasource.ErrCollectionNotWritable) {
			t.Fatalf("undeclared collection: %v, want ErrCollectionNotWritable", err)
		}
	})

	mt.Run("close releases the client", func(mt *mtest.T) {
		// A lazily connected real client: Connect performs no I/O, so
		// Close exercises the genuine disconnect path without a server.
		// (Disconnecting mt.Client itself would fight mtest cleanup over
		// the shared mock deployment.)
		client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://127.0.0.1:1"))
		if err != nil {
			t.Fatalf("building a lazy client: %v", err)
		}
		conn := newConnector(client, testSource())
		if err := conn.Close(ctx); err != nil {
			t.Fatalf("closing: %v", err)
		}
		if got := conn.Source(); got.Tenant != "tenant-a" || got.ID != "ledger" {
			t.Fatalf("connector source = %+v, want tenant-a/ledger", got)
		}
	})
}
