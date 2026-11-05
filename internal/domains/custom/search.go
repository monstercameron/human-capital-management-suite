package custom

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// SearchRequest describes a fail-closed projection read. Query keys and
// requested fields must both be authorized; an omitted grant never becomes a
// wildcard or a public default.
type SearchRequest struct {
	Tenant                values.TenantId
	Definition            CustomObjectDefinition
	Query                 map[string]string
	RequestedFields       []string
	GrantedDomains        map[string]bool
	Purpose               string
	MinimumSequence       int64
	RequiredSchemaVersion uint64
	// Principal and Access apply the definition's record rules. Nil Access
	// means no record filtering (field grants still apply). A non-nil Access
	// with record rules filters before paging; without rules the platform
	// policy alone decides each record.
	Principal Principal
	Access    *AccessDeps
	// Limit pages the filtered result; zero means no paging. Offset skips
	// that many filtered records. Negative values are rejected.
	Limit  int
	Offset int
}

// SearchRecord is the authorized semantic view of one projected record.
// Unauthorized fields are absent, not represented as empty values.
type SearchRecord struct {
	ObjectID          string
	Revision          uint64
	DefinitionVersion uint64
	Fields            map[string]TypedValue
	Digest            string
}

// SearchReport is complete only for the authorized semantic result. Freshness
// and schema metadata are always returned so consumers cannot mistake stale
// or differently-versioned data for a current answer.
type SearchReport struct {
	Tenant             values.TenantId
	Kind               string
	SchemaVersion      uint64
	SourceHead         int64
	ProjectionSequence int64
	Records            []SearchRecord
	Count              int
	Digest             string
}

func (r SearchReport) Explain() string {
	return fmt.Sprintf("custom search kind=%s schema_version=%d source_head=%d projection_sequence=%d records=%d digest=%s",
		r.Kind, r.SchemaVersion, r.SourceHead, r.ProjectionSequence, len(r.Records), shortDigest(r.Digest))
}

// Search implements exact equality filtering over the rebuildable projection.
// It never reads an unauthorized field to decide whether a row matches.
func (s *EventStore) Search(ctx context.Context, req SearchRequest) (SearchReport, error) {
	if err := ctx.Err(); err != nil {
		return SearchReport{}, err
	}
	if s == nil {
		return SearchReport{}, fmt.Errorf("%w: nil store", ErrInvalidMutation)
	}
	if req.Tenant == "" || req.Purpose == "" {
		return SearchReport{}, fmt.Errorf("%w: tenant and purpose are required", ErrInvalidMutation)
	}
	if err := req.Definition.Validate(); err != nil {
		return SearchReport{}, err
	}
	policy, err := ResolvePolicy(req.Definition)
	if err != nil {
		return SearchReport{}, err
	}
	authorized := make(map[string]bool, len(policy.Fields))
	for _, field := range policy.Fields {
		if req.GrantedDomains[field.AuthZDomain] {
			authorized[field.FieldName] = true
		}
	}
	fields := append([]string(nil), req.RequestedFields...)
	if len(fields) == 0 {
		for field := range authorized {
			fields = append(fields, field)
		}
	}
	sort.Strings(fields)
	fields = uniqueStrings(fields)
	for _, field := range fields {
		if !authorized[field] {
			return SearchReport{}, fmt.Errorf("%w: field %s", ErrCapabilityDenied, field)
		}
	}
	for field := range req.Query {
		if !authorized[field] {
			return SearchReport{}, fmt.Errorf("%w: query field %s", ErrCapabilityDenied, field)
		}
	}
	if req.RequiredSchemaVersion != 0 && req.RequiredSchemaVersion != req.Definition.Version {
		return SearchReport{}, fmt.Errorf("%w: required schema version %d, definition version %d", ErrInvalidMutation, req.RequiredSchemaVersion, req.Definition.Version)
	}

	if req.Limit < 0 || req.Offset < 0 {
		return SearchReport{}, fmt.Errorf("%w: limit and offset must be non-negative", ErrInvalidMutation)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	var sourceHead int64
	views := make([]SearchRecord, 0)
	var candidates []Event
	for key, event := range s.records {
		if !strings.HasPrefix(key, req.Tenant.String()+"\x00"+req.Definition.Kind+"\x00") || event.Retired {
			continue
		}
		streamHead := int64(len(s.events[streamKey(req.Tenant, req.Definition.Kind, event.ObjectID)]))
		if streamHead > sourceHead {
			sourceHead = streamHead
		}
		if req.MinimumSequence > streamHead {
			continue
		}
		if !matchesQuery(event.Record.FieldValues, req.Query) {
			continue
		}
		candidates = append(candidates, event)
	}
	// Record rules filter before paging: page boundaries are drawn over
	// allowed records only, so a page can never shift an unreadable record
	// into view.
	allowed := map[string]bool{}
	if req.Access != nil {
		records := make([]CustomRecordRevision, 0, len(candidates))
		for _, event := range candidates {
			records = append(records, event.Record)
		}
		filtered, err := Filter(ctx, req.Definition, req.Principal, records, req.Access)
		if err != nil {
			return SearchReport{}, err
		}
		for _, record := range filtered {
			allowed[record.ObjectID] = true
		}
	}
	for _, event := range candidates {
		if req.Access != nil && !allowed[event.ObjectID] {
			continue
		}
		view := SearchRecord{ObjectID: event.ObjectID, Revision: event.Revision, DefinitionVersion: event.DefinitionVersion, Fields: make(map[string]TypedValue, len(fields))}
		for _, field := range fields {
			view.Fields[field] = event.Record.FieldValues[field]
		}
		view.Digest = digestView(view)
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ObjectID < views[j].ObjectID })
	if req.MinimumSequence > sourceHead {
		return SearchReport{}, fmt.Errorf("%w: required sequence %d, source head %d", ErrProjectionStale, req.MinimumSequence, sourceHead)
	}
	total := len(views)
	if req.Offset > total {
		views = nil
	} else {
		views = views[req.Offset:]
	}
	if req.Limit > 0 && len(views) > req.Limit {
		views = views[:req.Limit]
	}
	report := SearchReport{Tenant: req.Tenant, Kind: req.Definition.Kind, SchemaVersion: req.Definition.Version,
		SourceHead: sourceHead, ProjectionSequence: sourceHead, Records: views, Count: total}
	report.Digest = digestReport(report)
	return cloneSearchReport(report), nil
}

func matchesQuery(fields map[string]TypedValue, query map[string]string) bool {
	for name, expected := range query {
		value, ok := fields[name]
		if !ok || fmt.Sprint(value.Value) != expected {
			return false
		}
	}
	return true
}

func digestView(view SearchRecord) string {
	parts := []string{view.ObjectID, fmt.Sprint(view.Revision), fmt.Sprint(view.DefinitionVersion)}
	keys := make([]string, 0, len(view.Fields))
	for key := range view.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key, view.Fields[key].Type, fmt.Sprintf("%v", view.Fields[key].Value))
	}
	return digestStrings(parts)
}
func digestReport(report SearchReport) string {
	parts := []string{report.Tenant.String(), report.Kind, fmt.Sprint(report.SchemaVersion), fmt.Sprint(report.SourceHead), fmt.Sprint(report.ProjectionSequence)}
	for _, record := range report.Records {
		parts = append(parts, record.ObjectID, fmt.Sprint(record.Revision), record.Digest)
	}
	return digestStrings(parts)
}
func cloneSearchReport(in SearchReport) SearchReport {
	in.Records = append([]SearchRecord(nil), in.Records...)
	for i := range in.Records {
		in.Records[i].Fields = copyValues(in.Records[i].Fields)
	}
	return in
}
func copyValues(in map[string]TypedValue) map[string]TypedValue {
	out := make(map[string]TypedValue, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
func uniqueStrings(in []string) []string {
	out := in[:0]
	for _, item := range in {
		if len(out) == 0 || out[len(out)-1] != item {
			out = append(out, item)
		}
	}
	return out
}
func shortDigest(value string) string {
	if len(value) > 16 {
		return value[:16]
	}
	return value
}
