// Package template re-exports the immutable document template contract from
// internal/doctemplate. The contract moved to a pure leaf so the browser client
// can link it without the documents engine; existing callers keep this path.
package template

import "github.com/monstercameron/human-capital-management-suite/internal/doctemplate"

type (
	State         = doctemplate.State
	ValueKind     = doctemplate.ValueKind
	Placeholder   = doctemplate.Placeholder
	Binding       = doctemplate.Binding
	Approval      = doctemplate.Approval
	Fixture       = doctemplate.Fixture
	Applicability = doctemplate.Applicability
	Definition    = doctemplate.Definition
	Template      = doctemplate.Template
	Publication   = doctemplate.Publication
	Artifact      = doctemplate.Artifact
)

const (
	Draft     = doctemplate.Draft
	Published = doctemplate.Published
	Retired   = doctemplate.Retired

	String   = doctemplate.String
	Integer  = doctemplate.Integer
	Decimal  = doctemplate.Decimal
	Boolean  = doctemplate.Boolean
	Date     = doctemplate.Date
	DateTime = doctemplate.DateTime
)

var (
	ErrInvalid              = doctemplate.ErrInvalid
	ErrNotPublished         = doctemplate.ErrNotPublished
	ErrChangedAfterApproval = doctemplate.ErrChangedAfterApproval
)

// New validates a definition and opens a draft template.
func New(d Definition) (Template, error) { return doctemplate.New(d) }
