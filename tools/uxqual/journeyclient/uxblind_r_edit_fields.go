package journeyclient

import (
	"github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// editProposalFields projects the correction dialog from the same governed
// field vocabulary as the new-promotion form. The temporary proposal fields
// deliberately pass through localizeProposalForm so labels, adornments,
// pay-range guidance and pay validation cannot drift between the two forms.
func editProposalFields(locale string, head journey.JourneyCard, options *journeyv1.WorkforceOptions) []journey.Field {
	worker := &journeyv1.Worker{
		JobCode: head.Edit.SourceJobCode, Grade: head.Edit.SourceGrade,
		BasePay: head.Edit.CurrentBase, Currency: head.Edit.Currency,
		PayBasis: head.Edit.CurrentPayBasis,
	}
	jobs, grades := governedProposalChoices(options, worker, head.Edit.JobCode)
	if len(jobs) == 0 && head.Edit.JobCode != "" {
		jobs = []string{head.Edit.JobCode}
	}
	if len(grades) == 0 && head.Edit.Grade != "" {
		grades = []string{head.Edit.Grade}
	}
	path := selectedPromotionPath(options, worker, head.Edit.JobCode, head.Edit.Grade)
	form := journey.ProposalForm{Fields: []journey.Field{
		{ID: FieldJobCode, Name: NameJobCode, Kind: kindSelect, Required: true, Value: head.Edit.JobCode, Options: promotionJobOptions(options, worker, jobs, head.Edit.JobCode)},
		{ID: FieldGrade, Name: NameGrade, Kind: kindSelect, Required: true, Value: head.Edit.Grade, Options: stringOptions("Select target grade", grades, head.Edit.Grade)},
		{ID: FieldBase, Name: NameBase, Kind: kindNumber, Required: true, Value: head.Edit.Base, Step: "0.01", Min: "0", Placeholder: "0.00"},
		{ID: FieldEffective, Name: NameEffective, Kind: kindDate, Required: true, Value: head.Edit.EffectiveISO},
		{ID: FieldReason, Name: NameReason, Kind: kindTextarea, Required: true, Value: head.Edit.BusinessReason},
	}}
	applyProposalCurrency(&form, head.Edit.Currency)
	copy := productui.ResolveProductLocale(locale)
	localizeProposalForm(&form, copy, true, path, proposalPayRangeFor(worker, options, path))

	fields := make([]journey.Field, 0, len(form.Fields)+1)
	for _, field := range form.Fields {
		switch field.ID {
		case FieldJobCode:
			field.Label = copy.Text("journey.iv_edit_job")
			field.ID, field.Name = FieldEditJobCode, NameEditJobCode
		case FieldGrade:
			field.ID, field.Name = FieldEditGrade, NameEditGrade
		case FieldBase:
			field.ID, field.Name = FieldEditBase, NameEditBase
		case FieldEffective:
			field.ID, field.Name = FieldEditEffective, NameEditEffective
		case FieldReason:
			field.ID, field.Name = FieldEditBusinessReason, NameEditBusinessReason
		}
		fields = append(fields, field)
	}
	fields = append(fields, journey.Field{
		ID: FieldEditReason, Name: NameEditReason, Label: copy.Text("journey.iv_edit_reason"),
		Kind: kindTextarea, Required: true, Placeholder: copy.Text("journey.iv_reason_placeholder"),
		Help: copy.Text("journey.iv_reason_help"),
	})
	return fields
}
