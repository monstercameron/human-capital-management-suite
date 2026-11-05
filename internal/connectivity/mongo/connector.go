package mongo

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/datasource"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// connector is the datasource.Connector over one driver client bound to
// one tenant source. The zero value is not usable.
type connector struct {
	src    datasource.Source
	client *mongo.Client
	db     *mongo.Database
}

// newConnector binds an established client to a validated source. The
// factory dials over the network; tests inject the driver's own mock
// client through this same constructor, so both paths share every line
// below it.
func newConnector(client *mongo.Client, src datasource.Source) *connector {
	return &connector{src: src, client: client, db: client.Database(src.Database)}
}

// Source returns the registry key this connector was opened for.
func (c *connector) Source() datasource.SourceID { return c.src.SourceID() }

// AdapterID names this adapter.
func (c *connector) AdapterID() string { return AdapterID }

func (c *connector) describe(op, collection string) string {
	return fmt.Sprintf("mongo: %s %s/%s %s", op, c.src.Tenant, c.src.ID, collection)
}

// Query parses the canonical-JSON predicate into a native filter, runs
// the find, and returns canonical-JSON documents.
func (c *connector) Query(ctx context.Context, q datasource.Query) ([]datasource.Document, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	filter, err := parsePredicate(q.Predicate)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.describe("query", q.Collection), err)
	}
	cur, err := c.db.Collection(q.Collection).Find(ctx, filter, options.Find().SetLimit(int64(q.EffectiveLimit())))
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %v", c.describe("query", q.Collection), datasource.ErrUnavailable, err)
	}
	var rows []bson.M
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("%s: %w: %v", c.describe("query", q.Collection), datasource.ErrUnavailable, err)
	}
	docs := make([]datasource.Document, 0, len(rows))
	for i, row := range rows {
		doc, err := canonicalJSON(row)
		if err != nil {
			return nil, fmt.Errorf("%s: row %d: %w", c.describe("query", q.Collection), i, err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// Write inserts one batch. Read-only sources and undeclared collections
// are refused before any document is parsed.
func (c *connector) Write(ctx context.Context, w datasource.Write) error {
	if err := w.Validate(); err != nil {
		return err
	}
	if c.src.ReadOnly {
		return fmt.Errorf("%w: %s/%s", datasource.ErrReadOnly, c.src.Tenant, c.src.ID)
	}
	if !c.src.Writable(w.Collection) {
		return fmt.Errorf("%w: %q", datasource.ErrCollectionNotWritable, w.Collection)
	}
	docs := make([]any, 0, len(w.Documents))
	for i, raw := range w.Documents {
		doc, err := parseDocument(raw)
		if err != nil {
			return fmt.Errorf("%s: document %d: %w", c.describe("write", w.Collection), i, err)
		}
		docs = append(docs, doc)
	}
	if _, err := c.db.Collection(w.Collection).InsertMany(ctx, docs); err != nil {
		return fmt.Errorf("%s: %w: %v", c.describe("write", w.Collection), datasource.ErrUnavailable, err)
	}
	return nil
}

// Close releases the underlying connection.
func (c *connector) Close(ctx context.Context) error {
	if err := c.client.Disconnect(ctx); err != nil {
		return fmt.Errorf("mongo: close %s/%s: %v", c.src.Tenant, c.src.ID, err)
	}
	return nil
}
