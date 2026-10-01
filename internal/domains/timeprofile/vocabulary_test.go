package timeprofile

import "testing"

func TestVocabularyRejectsUnknownTokens(t *testing.T) {
	if CaptureMode("CLOCK").Valid() || PayBasis("WEEKLY").Valid() || ExemptionStatus("MAYBE").Valid() ||
		WorkerCategory("VOLUNTEER").Valid() || Destination("BANK").Valid() || OvertimeMethod("DOUBLE").Valid() ||
		Template("time.other").Valid() {
		t.Fatal("an unknown token was accepted")
	}
}

func TestVocabularyParsesStoredTokens(t *testing.T) {
	if m, ok := ParseCaptureMode(" exception_only "); !ok || m != CaptureException {
		t.Fatalf("capture mode = %q, %v", m, ok)
	}
	if c, ok := ParseWorkerCategory("agency_temp"); !ok || c != CategoryAgencyTemp {
		t.Fatalf("category = %q, %v", c, ok)
	}
	if e, ok := ParseExemptionStatus("salaried_non_exempt"); !ok || e != SalariedNonExempt {
		t.Fatalf("exemption = %q, %v", e, ok)
	}
	if _, ok := ParseCaptureMode("punches"); ok {
		t.Fatal("parsed an unknown capture mode")
	}
}

func TestVocabularyEveryDeclaredValueIsValid(t *testing.T) {
	for _, v := range []CaptureMode{CapturePunch, CaptureDuration, CaptureException, CaptureNone} {
		if !v.Valid() {
			t.Fatalf("%q invalid", v)
		}
	}
	for _, v := range []PayBasis{PayHourly, PaySalary, PayPieceRate, PayDayRate, PayContract} {
		if !v.Valid() {
			t.Fatalf("%q invalid", v)
		}
	}
	for _, v := range []ExemptionStatus{NonExempt, Exempt, SalariedNonExempt, NotApplicable} {
		if !v.Valid() {
			t.Fatalf("%q invalid", v)
		}
	}
	for _, v := range []WorkerCategory{CategoryEmployee, CategoryContractor, CategoryAgencyTemp, CategoryPlatform} {
		if !v.Valid() {
			t.Fatalf("%q invalid", v)
		}
	}
	for _, v := range []Destination{DestinationPayroll, DestinationInvoice, DestinationAgency, DestinationCostingOnly} {
		if !v.Valid() {
			t.Fatalf("%q invalid", v)
		}
	}
	for _, v := range []OvertimeMethod{OvertimeNone, OvertimeSingleRate, OvertimeWeightedAverage, OvertimeFluctuatingWeek,
		OvertimeHealthcare880, OvertimePublicCompTime, OvertimePublicSafety7k, OvertimePieceRateAverage} {
		if !v.Valid() {
			t.Fatalf("%q invalid", v)
		}
	}
	for _, v := range []Template{TemplatePunchSession, TemplateDurationSheet, TemplateExceptionOnly, TemplateContractorTime, TemplateAgencyTime} {
		if !v.Valid() {
			t.Fatalf("%q invalid", v)
		}
	}
	if len(ControlClasses()) != 6 {
		t.Fatalf("control classes = %d", len(ControlClasses()))
	}
}
