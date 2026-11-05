// TestTodo_SMB_STOCK_010 covers SMB-STOCK-010: traceable physical returns
// with inspected disposition. Receipts land quarantine-held by default and
// never become usable before inspection; the returnable allowance is
// consumed atomically at physical receipt; serial/lot identity must match
// the original issue; stock disposition never executes a financial remedy.
package stock

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func stock010Context() OperationContext {
	return OperationContext{TenantID: "tenant-a", CompanyID: "company-a", ActorID: "clerk-1", Origin: OriginHuman}
}

func stock010At() time.Time {
	return time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
}

func stock010Dec(t *testing.T, s string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(s, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func stock010Int(t *testing.T, s string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(s, 0, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func stock010Binding() *AssetBinding {
	return &AssetBinding{AssetID: "asset-pump-7", CustodyRev: "custody-3", ServiceabilityRev: "svc-2", MovementOp: "move-r1"}
}

func stock010Issue(t *testing.T, reg *ReturnRegistry, op OperationContext, lineID, qty string) IssueLine {
	t.Helper()
	line, err := reg.RecordIssueLine(op, RecordIssueInput{
		LineID: lineID, SiteID: "SITE-A", ItemID: "item-1", Quantity: stock010Dec(t, qty),
		LotID: "LOT-1", SourceKind: ReturnSourceJobIssue, SourceRef: "job-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func stock010Receive(t *testing.T, reg *ReturnRegistry, op OperationContext, req ReceiveReturnInput) MaterialReceipt {
	t.Helper()
	receipt, err := reg.ReceiveMaterialReturn(op, req)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}
func TestTodo_SMB_STOCK_010(t *testing.T) {
	reg := NewReturnRegistry(nil)
	op := stock010Context()
	line := stock010Issue(t, reg, op, "issue-1", "5.00")

	ret, err := reg.BeginMaterialReturn(op, "begin-p1", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "5.00"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt := stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-p1",
		Quantity: stock010Dec(t, "2.00"), LotID: "LOT-1", Condition: ReturnConditionDamaged,
		Evidence: "photo-damage-1", Actor: "receiver-1", At: stock010At(),
	})
	if receipt.QuarantineHeld.String() != "2.00" || receipt.Accepted.String() != "0.00" {
		t.Fatalf("damaged receipt buckets = %+v, want quarantine-held 2 accepted 0", receipt)
	}
	statement, err := reg.LineStatement(op, line.LineID)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Issued.String() != "5.00" || statement.Returned.String() != "2.00" || statement.Net.String() != "3.00" {
		t.Fatalf("line statement = %+v, want issued 5 returned 2 net 3", statement)
	}
	allowance, err := reg.ReturnableAllowance(op, line.LineID)
	if err != nil || allowance.String() != "3.00" {
		t.Fatalf("allowance = %v, err=%v; want 3.00 immediately, before inspection", allowance, err)
	}
	if _, err := reg.BeginMaterialReturn(op, "begin-p1-over", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "4.00"), ReturningParty: "crew-3", At: stock010At(),
	}); !errors.Is(err, ErrReturnValidation) {
		t.Fatalf("over-allowance authorization err=%v, want validation refusal", err)
	}

	serialLine, err := reg.RecordIssueLine(op, RecordIssueInput{
		LineID: "issue-s1", SiteID: "SITE-A", ItemID: "item-pump", Quantity: stock010Int(t, "2"),
		Serials: []string{"PUMP-1", "PUMP-2"}, SourceKind: ReturnSourceJobIssue, SourceRef: "job-8",
	})
	if err != nil {
		t.Fatal(err)
	}
	serialRet, err := reg.BeginMaterialReturn(op, "begin-p1s", BeginReturnInput{
		LineID: serialLine.LineID, Requested: stock010Int(t, "2"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.ReceiveMaterialReturn(op, ReceiveReturnInput{
		ReturnID: serialRet.ReturnID, ExpectedRevision: serialRet.Revision, Key: "recv-p1s-unbound",
		Quantity: stock010Int(t, "1"), Serials: []string{"PUMP-1"}, Condition: ReturnConditionUnopened,
		Actor: "receiver-1", At: stock010At(),
	}); !errors.Is(err, ErrAssetBindingRequired) {
		t.Fatalf("unbound serialized receive err=%v, want binding refusal", err)
	}
	if _, err := reg.ReceiveMaterialReturn(op, ReceiveReturnInput{
		ReturnID: serialRet.ReturnID, ExpectedRevision: serialRet.Revision, Key: "recv-p1s-wrong",
		Quantity: stock010Int(t, "1"), Serials: []string{"PUMP-9"}, Condition: ReturnConditionUnopened,
		Actor: "receiver-1", At: stock010At(), Binding: stock010Binding(),
	}); !errors.Is(err, ErrReturnValidation) {
		t.Fatalf("wrong-serial receive err=%v, want validation refusal", err)
	}
	allowanceS, err := reg.ReturnableAllowance(op, serialLine.LineID)
	if err != nil || allowanceS.String() != "2" {
		t.Fatalf("refused receives left allowance %v, err=%v; want 2 with zero stock change", allowanceS, err)
	}
	bound := stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: serialRet.ReturnID, ExpectedRevision: serialRet.Revision, Key: "recv-p1s",
		Quantity: stock010Int(t, "1"), Serials: []string{"PUMP-1"}, Condition: ReturnConditionUnopened,
		Actor: "receiver-1", At: stock010At(), Binding: stock010Binding(),
	})
	if bound.QuarantineHeld.String() != "1" {
		t.Fatalf("bound serialized receipt held %s, want 1", bound.QuarantineHeld)
	}
}
func TestTodo_SMB_STOCK_010_Integration(t *testing.T) {
	reg := NewReturnRegistry(nil)
	op := stock010Context()
	line := stock010Issue(t, reg, op, "issue-i1", "5.00")
	ret, err := reg.BeginMaterialReturn(op, "begin-i1", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "3.00"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt := stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-i1",
		Quantity: stock010Dec(t, "3.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
		Actor: "receiver-1", At: stock010At(),
	})
	if _, err := reg.InspectMaterialReturn(op, InspectReturnInput{
		ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, Key: "insp-i1-self",
		Accepted: stock010Dec(t, "2.00"), Quarantined: stock010Dec(t, "1.00"),
		Inspector: "receiver-1", At: stock010At(),
	}); !errors.Is(err, ErrReturnValidation) {
		t.Fatalf("self-inspection err=%v, want maker-checker refusal", err)
	}
	inspection, err := reg.InspectMaterialReturn(op, InspectReturnInput{
		ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, Key: "insp-i1",
		Accepted: stock010Dec(t, "2.00"), Quarantined: stock010Dec(t, "1.00"),
		Inspector: "inspector-2", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Accepted.String() != "2.00" || inspection.Quarantined.String() != "1.00" {
		t.Fatalf("inspection split = %+v, want accepted 2 quarantined 1", inspection)
	}
	if _, err := reg.InspectMaterialReturn(op, InspectReturnInput{
		ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, Key: "insp-i1-dup",
		Accepted: stock010Dec(t, "3.00"), Inspector: "inspector-2", At: stock010At(),
	}); !errors.Is(err, ErrReturnValidation) {
		t.Fatalf("re-inspection err=%v, want refusal", err)
	}
	resolution, err := reg.ResolveReturn(op, ResolveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: inspection.ReturnRevision, Key: "resolve-i1",
		Remedy: ReturnRemedyRestock, Actor: "clerk-1", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolution.Restock) != 1 || resolution.Restock[0].Quantity.String() != "2.00" || resolution.Restock[0].LotID != "LOT-1" {
		t.Fatalf("restock intents = %+v, want one 2.00 LOT-1 intent", resolution.Restock)
	}
	if resolution.Buckets.Received.String() != "3.00" || resolution.Buckets.Accepted.String() != "2.00" {
		t.Fatalf("resolution buckets = %+v, want received 3 accepted 2", resolution.Buckets)
	}
	payload, err := json.Marshal(resolution)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(payload)), "refund") {
		t.Fatalf("resolution leaks financial remedy state: %s", payload)
	}

	shipLine, err := reg.RecordIssueLine(op, RecordIssueInput{
		LineID: "ship-i2", SiteID: "SITE-A", ItemID: "item-1", Quantity: stock010Dec(t, "5.00"),
		LotID: "LOT-9", SourceKind: ReturnSourceCustomerShipment, SourceRef: "order-42", OrderReceipt: "order-rcpt-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	shipRet, err := reg.BeginMaterialReturn(op, "begin-i2", BeginReturnInput{
		LineID: shipLine.LineID, Requested: stock010Dec(t, "2.00"), ReturningParty: "customer-1", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	shipReceipt := stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: shipRet.ReturnID, ExpectedRevision: shipRet.Revision, Key: "recv-i2",
		Quantity: stock010Dec(t, "2.00"), LotID: "LOT-9", Condition: ReturnConditionDamaged,
		Evidence: "photo-9", Actor: "receiver-1", At: stock010At(),
	})
	allowance, err := reg.ReturnableAllowance(op, shipLine.LineID)
	if err != nil || allowance.String() != "3.00" {
		t.Fatalf("customer-return allowance = %v, err=%v; want 3.00", allowance, err)
	}
	if _, err := reg.InspectMaterialReturn(op, InspectReturnInput{
		ReturnID: shipRet.ReturnID, ReceiptID: shipReceipt.ReceiptID, Key: "insp-i2",
		Rejected: stock010Dec(t, "2.00"), ReturnShipmentRef: "ship-back-1", Inspector: "inspector-2", At: stock010At(),
	}); err != nil {
		t.Fatal(err)
	}
	shipResolution, err := reg.ResolveReturn(op, ResolveReturnInput{
		ReturnID: shipRet.ReturnID, ExpectedRevision: shipRet.Revision + 2, Key: "resolve-i2",
		Remedy: ReturnRemedyCreditRequest, Actor: "clerk-1", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if shipResolution.Credit == nil || shipResolution.Credit.OrderReceipt != "order-rcpt-42" || shipResolution.Credit.ReturnID != shipRet.ReturnID {
		t.Fatalf("credit linkage = %+v, want one order-rcpt-42 link", shipResolution.Credit)
	}
	allowance, err = reg.ReturnableAllowance(op, shipLine.LineID)
	if err != nil || allowance.String() != "3.00" {
		t.Fatalf("rejected return restored allowance to %v, want it to stay consumed at 3.00", allowance)
	}
}
func TestTodo_SMB_STOCK_010_Race(t *testing.T) {
	reg := NewReturnRegistry(nil)
	op := stock010Context()
	line := stock010Issue(t, reg, op, "issue-r1", "5.00")
	retA, err := reg.BeginMaterialReturn(op, "begin-r1a", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "3.00"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Second authorization commits the remaining allowance: the
	// shared line fence at receive still caps total physical receipts.
	retB, err := reg.BeginMaterialReturn(op, "begin-r1b", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "2.00"), ReturningParty: "crew-4", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var wins atomic.Int64
	var refused atomic.Int64
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			_, err := reg.ReceiveMaterialReturn(op, ReceiveReturnInput{
				ReturnID: retA.ReturnID, ExpectedRevision: retA.Revision, Key: "recv-race-1",
				Quantity: stock010Dec(t, "3.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
				Actor: "receiver-1", At: stock010At(),
			})
			_ = slot
			if err == nil {
				wins.Add(1)
			} else if errors.Is(err, ErrReturnValidation) || errors.Is(err, ErrReturnConflict) {
				refused.Add(1)
			} else {
				t.Errorf("contended receive err=%v, want success or fence refusal", err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 2 || refused.Load() != 0 {
		t.Fatalf("same-key contended receive wins=%d refused=%d, want 2 idempotent wins", wins.Load(), refused.Load())
	}
	statement, err := reg.LineStatement(op, line.LineID)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Returned.String() != "3.00" {
		t.Fatalf("contended receives returned %s, want exactly one 3.00 effect", statement.Returned)
	}

	var fenceWins atomic.Int64
	var fenceRefused atomic.Int64
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			key := "recv-race-fence-a"
			if slot == 1 {
				key = "recv-race-fence-b"
			}
			_, err := reg.ReceiveMaterialReturn(op, ReceiveReturnInput{
				ReturnID: retB.ReturnID, ExpectedRevision: retB.Revision, Key: key,
				Quantity: stock010Dec(t, "2.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
				Actor: "receiver-1", At: stock010At(),
			})
			if err == nil {
				fenceWins.Add(1)
			} else if errors.Is(err, ErrReturnValidation) || errors.Is(err, ErrReturnConflict) {
				fenceRefused.Add(1)
			} else {
				t.Errorf("fence race err=%v, want success or fence refusal", err)
			}
		}(i)
	}
	wg.Wait()
	statement, err = reg.LineStatement(op, line.LineID)
	if err != nil {
		t.Fatal(err)
	}
	if statement.Returned.String() != "5.00" || fenceWins.Load() != 1 || fenceRefused.Load() != 1 {
		t.Fatalf("fence race returned %s wins=%d refused=%d, want 5.00 with 1 win 1 refusal", statement.Returned, fenceWins.Load(), fenceRefused.Load())
	}
}

func TestTodo_SMB_STOCK_010_Security(t *testing.T) {
	allowUnlinked := func(op OperationContext, permissionRef string) error { return nil }
	denyUnlinked := func(op OperationContext, permissionRef string) error { return ErrReturnValidation }
	reg := NewReturnRegistry(allowUnlinked)
	op := stock010Context()
	line := stock010Issue(t, reg, op, "issue-x1", "5.00")
	ret, err := reg.BeginMaterialReturn(op, "begin-x1", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "2.00"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}

	foreign := OperationContext{TenantID: "tenant-b", CompanyID: "company-a", ActorID: "intruder", Origin: OriginHuman}
	if _, err := reg.GetReturn(foreign, ret.ReturnID); !errors.Is(err, ErrStockScope) {
		t.Fatalf("cross-tenant read err=%v, want neutral scope refusal", err)
	}
	if _, err := reg.GetReturn(op, "ret-guessed"); !errors.Is(err, ErrStockScope) {
		t.Fatalf("guessed id read err=%v, want the same neutral scope refusal", err)
	}
	if _, err := reg.ReturnableAllowance(foreign, line.LineID); !errors.Is(err, ErrStockScope) {
		t.Fatalf("cross-tenant allowance err=%v, want neutral scope refusal", err)
	}
	if got := reg.ListReturns(foreign, ""); len(got) != 0 {
		t.Fatalf("cross-tenant list disclosed %d returns", len(got))
	}
	siteB := OperationContext{TenantID: "tenant-a", CompanyID: "company-a", SiteID: "SITE-B", ActorID: "viewer-1", Origin: OriginHuman}
	if _, err := reg.GetReturn(siteB, ret.ReturnID); !errors.Is(err, ErrStockScope) {
		t.Fatalf("denied-site read err=%v, want neutral scope refusal", err)
	}

	denied := NewReturnRegistry(denyUnlinked)
	if _, err := denied.BeginUnlinkedReturn(op, "begin-xu-1", BeginUnlinkedInput{
		SiteID: "SITE-A", ItemID: "item-1", Quantity: stock010Dec(t, "1.00"),
		PermissionRef: "perm-granted", Evidence: "evidence-1", ReturningParty: "crew-3", At: stock010At(),
	}); !errors.Is(err, ErrReturnValidation) {
		t.Fatalf("denied unlinked permission err=%v, want validation refusal", err)
	}
	if _, err := reg.BeginUnlinkedReturn(op, "begin-xu-2", BeginUnlinkedInput{
		SiteID: "SITE-A", ItemID: "item-1", Quantity: stock010Dec(t, "1.00"),
		Evidence: "evidence-1", ReturningParty: "crew-3", At: stock010At(),
	}); !errors.Is(err, ErrReturnValidation) {
		t.Fatalf("unlinked return without permission ref err=%v, want validation refusal", err)
	}
	unlinked, err := reg.BeginUnlinkedReturn(op, "begin-xu-3", BeginUnlinkedInput{
		SiteID: "SITE-A", ItemID: "item-1", Quantity: stock010Dec(t, "1.00"),
		PermissionRef: "perm-granted", Evidence: "evidence-1", ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil || unlinked.LineID != "" {
		t.Fatalf("authorized unlinked begin = %+v, err=%v; want a lineless return", unlinked, err)
	}

	stale := stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-x1",
		Quantity: stock010Dec(t, "1.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
		Actor: "receiver-1", At: stock010At(),
	})
	_ = stale
	if _, err := reg.ReceiveMaterialReturn(op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-x1-stale",
		Quantity: stock010Dec(t, "1.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
		Actor: "receiver-1", At: stock010At(),
	}); !errors.Is(err, ErrReturnConflict) {
		t.Fatalf("stale revision receive err=%v, want conflict", err)
	}
}
func TestTodo_SMB_STOCK_010_Recovery(t *testing.T) {
	reg := NewReturnRegistry(nil)
	op := stock010Context()
	line := stock010Issue(t, reg, op, "issue-v1", "5.00")
	ret, err := reg.BeginMaterialReturn(op, "begin-v1", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "2.00"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-v1",
		Quantity: stock010Dec(t, "2.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
		Actor: "receiver-1", At: stock010At(),
	})
	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewReturnRegistry(nil)
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	allowance, err := restored.ReturnableAllowance(op, line.LineID)
	if err != nil || allowance.String() != "3.00" {
		t.Fatalf("restarted allowance = %v, err=%v; want 3.00", allowance, err)
	}
	replay, err := restored.ReceiveMaterialReturn(op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-v1",
		Quantity: stock010Dec(t, "2.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
		Actor: "receiver-1", At: stock010At(),
	})
	if err != nil || replay.QuarantineHeld.String() != "2.00" {
		t.Fatalf("post-restart replay = %+v, err=%v; want the original receipt", replay, err)
	}
	statement, err := restored.LineStatement(op, line.LineID)
	if err != nil || statement.Returned.String() != "2.00" {
		t.Fatalf("post-restart replay returned %s, want exactly one 2.00 effect", statement.Returned)
	}
	if _, err := restored.ReceiveMaterialReturn(op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-v1",
		Quantity: stock010Dec(t, "1.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
		Actor: "receiver-1", At: stock010At(),
	}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("reused key with changed payload err=%v, want idempotency conflict", err)
	}
	if err := restored.Import([]byte("{not-json")); err == nil {
		t.Fatal("corrupt snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_STOCK_010_SnapshotBindingRefused(t *testing.T) {
	reg := NewReturnRegistry(nil)
	op := stock010Context()
	line := stock010Issue(t, reg, op, "issue-n1", "5.00")
	ret, err := reg.BeginMaterialReturn(op, "begin-n1", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "2.00"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-n1",
		Quantity: stock010Dec(t, "2.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
		Actor: "receiver-1", At: stock010At(),
	})
	snapshot, err := reg.Export()
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(t *testing.T, change func(decoded map[string]any), want string) {
		t.Helper()
		var decoded map[string]any
		if err := json.Unmarshal(snapshot, &decoded); err != nil {
			t.Fatal(err)
		}
		change(decoded)
		raw, err := json.Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}
		if err := NewReturnRegistry(nil).Import(raw); err == nil {
			t.Fatalf("%s: tampered snapshot import succeeded, want refusal", want)
		}
	}

	mutate(t, func(decoded map[string]any) {
		returns := decoded["returns"].([]any)
		returns[0].(map[string]any)["line_id"] = "issue-other"
	}, "return/line binding mismatch")
	mutate(t, func(decoded map[string]any) {
		returns := decoded["returns"].([]any)
		receipts := returns[0].(map[string]any)["receipts"].([]any)
		receipts[0].(map[string]any)["quantity"] = "99.00/EXACT"
	}, "receipt quantity tamper")
	mutate(t, func(decoded map[string]any) {
		for key := range decoded["idem"].(map[string]any) {
			decoded["idem"].(map[string]any)[key+"-forged"] = decoded["idem"].(map[string]any)[key]
			delete(decoded["idem"].(map[string]any), key)
			break
		}
	}, "idempotency key refile")
	mutate(t, func(decoded map[string]any) {
		decoded["seq"] = float64(0)
	}, "regressed sequence")
	_ = line
}

func TestTodo_SMB_STOCK_010_Browser(t *testing.T) {
	reg := NewReturnRegistry(nil)
	op := stock010Context()
	line := stock010Issue(t, reg, op, "issue-b1", "5.00")
	ret, err := reg.BeginMaterialReturn(op, "begin-b1", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "2.00"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt := stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-b1",
		Quantity: stock010Dec(t, "2.00"), LotID: "LOT-1", Condition: ReturnConditionDamaged,
		Evidence: "photo-1", Actor: "receiver-1", At: stock010At(),
	})
	rows := reg.ProjectReturnQueue(op, "")
	if len(rows) != 1 || rows[0].Status != "AWAITING_INSPECTION" || rows[0].ReturnID != ret.ReturnID {
		t.Fatalf("return queue = %+v, want one awaiting-inspection row", rows)
	}
	if rows[0].RemainingReturnable != "3.00" {
		t.Fatalf("queue remaining returnable = %q, want 3.00", rows[0].RemainingReturnable)
	}
	preview := reg.ProjectInspectionPreview(op, ret.ReturnID, receipt.ReceiptID, InspectChoice{
		Accepted: stock010Dec(t, "1.00"), Quarantined: stock010Dec(t, "1.00"),
	})
	if preview.Error != "" {
		t.Fatalf("balanced preview errored: %q", preview.Error)
	}
	if preview.ResultingUsable != "1.00" || preview.ResultingQuarantine != "1.00" {
		t.Fatalf("preview buckets = %+v, want usable 1 quarantine 1", preview)
	}
	bad := reg.ProjectInspectionPreview(op, ret.ReturnID, receipt.ReceiptID, InspectChoice{
		Accepted: stock010Dec(t, "2.00"), Quarantined: stock010Dec(t, "1.00"),
	})
	if bad.Error == "" {
		t.Fatal("over-split preview succeeded, want an explicit error")
	}
	remedy := reg.ProjectRemedyStatus(op, ret.ReturnID, "credit-request", false)
	if remedy.State != "remedy-pending-outside-inventory" || remedy.Completed {
		t.Fatalf("remedy projection = %+v, want pending outside inventory, never completed", remedy)
	}
	denied := OperationContext{TenantID: "tenant-a", CompanyID: "company-a", SiteID: "SITE-B", ActorID: "viewer-1", Origin: OriginHuman}
	if rows := reg.ProjectReturnQueue(denied, ""); len(rows) != 0 {
		t.Fatalf("denied-site queue disclosed %d rows", len(rows))
	}
}
func TestTodo_SMB_STOCK_010_Cancel(t *testing.T) {
	reg := NewReturnRegistry(nil)
	op := stock010Context()
	line := stock010Issue(t, reg, op, "issue-c1", "5.00")
	ret, err := reg.BeginMaterialReturn(op, "begin-c1", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "5.00"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := reg.CancelReturn(op, CancelReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "cancel-c1",
		Reason: "crew kept the parts", Actor: "clerk-1", At: stock010At(),
	})
	if err != nil || cancelled.Status != ReturnStatusClosed || !cancelled.Cancelled {
		t.Fatalf("cancel = %+v, err=%v; want closed cancellation", cancelled, err)
	}
	full, err := reg.BeginMaterialReturn(op, "begin-c1b", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Dec(t, "5.00"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatalf("cancel did not release the held authorization: %v", err)
	}
	stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: full.ReturnID, ExpectedRevision: full.Revision, Key: "recv-c1",
		Quantity: stock010Dec(t, "1.00"), LotID: "LOT-1", Condition: ReturnConditionUsed,
		Actor: "receiver-1", At: stock010At(),
	})
	if _, err := reg.CancelReturn(op, CancelReturnInput{
		ReturnID: full.ReturnID, ExpectedRevision: full.Revision + 1, Key: "cancel-c1-late",
		Reason: "too late", Actor: "clerk-1", At: stock010At(),
	}); !errors.Is(err, ErrReturnValidation) {
		t.Fatalf("cancel after receipt err=%v, want disposition refusal", err)
	}
}

func TestTodo_SMB_STOCK_010_Discrepancy(t *testing.T) {
	reg := NewReturnRegistry(nil)
	op := stock010Context()
	line, err := reg.RecordIssueLine(op, RecordIssueInput{
		LineID: "issue-d1", SiteID: "SITE-A", ItemID: "item-pump", Quantity: stock010Int(t, "2"),
		Serials: []string{"PUMP-1", "PUMP-2"}, SourceKind: ReturnSourceJobIssue, SourceRef: "job-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	ret, err := reg.BeginMaterialReturn(op, "begin-d1", BeginReturnInput{
		LineID: line.LineID, Requested: stock010Int(t, "2"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.ReceiveMaterialReturn(op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "recv-d1-wrong",
		Quantity: stock010Int(t, "1"), Serials: []string{"PUMP-9"}, Condition: ReturnConditionUnopened,
		Actor: "receiver-1", At: stock010At(), Binding: stock010Binding(),
	}); !errors.Is(err, ErrReturnValidation) {
		t.Fatalf("wrong-serial receive err=%v, want validation refusal", err)
	}
	parked, err := reg.EscalateDiscrepancy(op, EscalateDiscrepancyInput{
		ReturnID: ret.ReturnID, ExpectedRevision: ret.Revision, Key: "esc-d1",
		PresentedSerials: []string{"PUMP-9"}, Reason: "label swapped in field", Evidence: "photo-swap",
		Actor: "receiver-1", At: stock010At(),
	})
	if err != nil || parked.Status != ReturnStatusDiscrepancy {
		t.Fatalf("escalate = %+v, err=%v; want parked discrepancy", parked, err)
	}
	if _, err := reg.ReceiveMaterialReturn(op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: parked.Revision, Key: "recv-d1-blocked",
		Quantity: stock010Int(t, "1"), Serials: []string{"PUMP-1"}, Condition: ReturnConditionUnopened,
		Actor: "receiver-1", At: stock010At(), Binding: stock010Binding(),
	}); !errors.Is(err, ErrReturnValidation) {
		t.Fatalf("receive while parked err=%v, want validation refusal", err)
	}
	decided, err := reg.ResolveDiscrepancy(op, ResolveDiscrepancyInput{
		ReturnID: ret.ReturnID, ExpectedRevision: parked.Revision, Key: "dec-d1",
		DiscrepancyKey: "esc-d1", Decision: DiscrepancyAcceptSubstitute,
		Reviewer: "supervisor-1", Evidence: "verified-swap", At: stock010At(),
	})
	if err != nil || decided.Status != ReturnStatusAuthorized {
		t.Fatalf("substitute decision = %+v, err=%v; want authorized with substitute", decided, err)
	}
	substitute := stock010Receive(t, reg, op, ReceiveReturnInput{
		ReturnID: ret.ReturnID, ExpectedRevision: decided.Revision, Key: "recv-d1-sub",
		Quantity: stock010Int(t, "1"), Serials: []string{"PUMP-9"}, Condition: ReturnConditionUnopened,
		Actor: "receiver-1", At: stock010At(), Binding: stock010Binding(),
	})
	if len(substitute.Serials) != 1 || substitute.Serials[0] != "PUMP-9" {
		t.Fatalf("substitute receipt = %+v, want the authorized PUMP-9", substitute)
	}

	line2, err := reg.RecordIssueLine(op, RecordIssueInput{
		LineID: "issue-d2", SiteID: "SITE-A", ItemID: "item-pump", Quantity: stock010Int(t, "2"),
		Serials: []string{"PUMP-3", "PUMP-4"}, SourceKind: ReturnSourceJobIssue, SourceRef: "job-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	ret2, err := reg.BeginMaterialReturn(op, "begin-d2", BeginReturnInput{
		LineID: line2.LineID, Requested: stock010Int(t, "1"), ReturningParty: "crew-3", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	parked2, err := reg.EscalateDiscrepancy(op, EscalateDiscrepancyInput{
		ReturnID: ret2.ReturnID, ExpectedRevision: ret2.Revision, Key: "esc-d2",
		PresentedSerials: []string{"PUMP-8"}, Reason: "unknown unit", Evidence: "photo-8",
		Actor: "receiver-1", At: stock010At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := reg.ResolveDiscrepancy(op, ResolveDiscrepancyInput{
		ReturnID: ret2.ReturnID, ExpectedRevision: parked2.Revision, Key: "dec-d2",
		DiscrepancyKey: "esc-d2", Decision: DiscrepancyReject,
		Reviewer: "supervisor-1", Evidence: "not-ours", At: stock010At(),
	})
	if err != nil || rejected.Status != ReturnStatusAuthorized || len(rejected.SubstituteSer) != 0 {
		t.Fatalf("reject decision = %+v, err=%v; want authorized with no substitute", rejected, err)
	}
	allowance, err := reg.ReturnableAllowance(op, line.LineID)
	if err != nil || allowance.String() != "1" {
		t.Fatalf("allowance = %v, err=%v; want 1 (only the substitute receipt consumed)", allowance, err)
	}
}

func TestTodo_SMB_STOCK_010_IssueIdempotent(t *testing.T) {
	reg := NewReturnRegistry(nil)
	op := stock010Context()
	input := RecordIssueInput{
		LineID: "issue-e1", SiteID: "SITE-A", ItemID: "item-1", Quantity: stock010Dec(t, "5.00"),
		LotID: "LOT-1", SourceKind: ReturnSourceJobIssue, SourceRef: "job-7",
	}
	first, err := reg.RecordIssueLine(op, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.RecordIssueLine(op, input)
	if err != nil || second.LineID != first.LineID {
		t.Fatalf("identical re-record = %+v, err=%v; want the recorded line", second, err)
	}
	changed := input
	changed.Quantity = stock010Dec(t, "6.00")
	if _, err := reg.RecordIssueLine(op, changed); !errors.Is(err, ErrReturnConflict) {
		t.Fatalf("changed re-record err=%v, want conflict", err)
	}
	foreign := OperationContext{TenantID: "tenant-b", CompanyID: "company-a", ActorID: "x", Origin: OriginHuman}
	if _, err := reg.RecordIssueLine(foreign, input); err != nil {
		t.Fatalf("same line id in another tenant refused: %v", err)
	}
}
