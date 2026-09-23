package pgstore

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// corruptObligationsRow is a dbport.Row whose legal-evidence blobs are not
// JSON. scanRecord must refuse it: a corrupt obligations blob is a corrupt
// projection, not an empty one.
type corruptObligationsRow struct {
	applied    []byte
	discharges []byte
}

func (r corruptObligationsRow) Scan(dest ...any) error {
	if len(dest) != 24 {
		return errFakeRowShape
	}
	for _, d := range dest {
		rv := reflect.ValueOf(d)
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			return errFakeRowShape
		}
		rv.Elem().Set(reflect.Zero(rv.Elem().Type()))
	}
	// Positions follow scanRecord's Scan call: 17 is legalReceiptRef
	// (**string, must be non-nil to enter the decode block), 22 and 23 are
	// the applied-obligations and discharges blobs (*[]byte).
	receipt, ok := dest[17].(**string)
	if !ok {
		return errFakeRowShape
	}
	*receipt = new(string)
	**receipt = "receipt:rev10306"
	applied, ok := dest[22].(*[]byte)
	if !ok {
		return errFakeRowShape
	}
	*applied = r.applied
	discharges, ok := dest[23].(*[]byte)
	if !ok {
		return errFakeRowShape
	}
	*discharges = r.discharges
	return nil
}

var errFakeRowShape = errorString("pgstore test: fake row shape does not match scanRecord")

type errorString string

func (e errorString) Error() string { return string(e) }

// TestTodo_REV_103_06 proves a corrupt legal-obligations blob fails the
// read instead of decoding to zero: the caller gets an error naming the
// corrupt field, never a record with laundered legal evidence.
func TestTodo_REV_103_06(t *testing.T) {
	store := &Store{}
	_, err := store.scanRecord(context.Background(), "tenant:acme", uuid.New(),
		corruptObligationsRow{applied: []byte("{not json"), discharges: []byte(`[]`)})
	if err == nil || !strings.Contains(err.Error(), "applied legal obligations") {
		t.Fatalf("corrupt applied obligations err = %v, want a decode error naming the field", err)
	}
}

// TestTodo_REV_103_06_Fault proves the discharges blob is load bearing too:
// corrupting it alone fails the read the same way.
func TestTodo_REV_103_06_Fault(t *testing.T) {
	store := &Store{}
	_, err := store.scanRecord(context.Background(), "tenant:acme", uuid.New(),
		corruptObligationsRow{applied: []byte(`[]`), discharges: []byte("[unclosed")})
	if err == nil || !strings.Contains(err.Error(), "legal obligation discharges") {
		t.Fatalf("corrupt discharges err = %v, want a decode error naming the field", err)
	}
}
