package productui

// CHATBUG-014: the provenance display named its two vocabularies by importing
// them: the lineage verdict from internal/data/provenance, the package that
// reads and publishes lineage in the database, and the authority kind from the
// generated evidence messages. A type and a few constants brought both
// packages into the browser client, and with the first its own imports: the
// ledger with its PostgreSQL driver types, the outbox, tenancy with its YAML
// reader. None of that can run in a browser, all of it was initialised when
// the client started, and it put the client over its size ceiling. The page
// keeps its own names for the values instead; they are the canonical ones,
// which a test holds them to.

// ProvenanceLineage is the completeness verdict of a provenance graph, as the
// page shows it. Its values are those of internal/data/provenance.Status.
type ProvenanceLineage string

const (
	// ProvenanceLineageComplete: every ledger event has its provenance record.
	ProvenanceLineageComplete ProvenanceLineage = "COMPLETE"
	// ProvenanceLineagePartial: at least one ledger event has none yet.
	ProvenanceLineagePartial ProvenanceLineage = "PARTIAL"
	// ProvenanceLineageUnknown: there are no events to judge completeness by.
	ProvenanceLineageUnknown ProvenanceLineage = "UNKNOWN"
)

// ProvenanceAuthority says what kind of source a value came from, as the page
// shows it. Its values are the numbers of the evidence messages' AuthorityKind.
type ProvenanceAuthority int32

const (
	// ProvenanceAuthorityLocal: this system is the authority for the value.
	ProvenanceAuthorityLocal ProvenanceAuthority = 1
	// ProvenanceAuthorityExternal: the value was observed in another system.
	ProvenanceAuthorityExternal ProvenanceAuthority = 2
	// ProvenanceAuthorityDerived: the value was computed from others.
	ProvenanceAuthorityDerived ProvenanceAuthority = 3
)
