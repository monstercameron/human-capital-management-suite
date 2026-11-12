package jurisdiction

import (
	"bytes"
	"errors"
	"testing"
)

func TestValidateAcceptsWellFormedFacts(t *testing.T) {
	for _, j := range []Jurisdiction{
		{Country: "US"},
		{Country: "US", State: "CA"},
		{Country: "US", State: "NY", Locality: "New York City"},
	} {
		if err := j.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", j, err)
		}
	}
}

func TestValidateRefusesMalformedFacts(t *testing.T) {
	cases := []struct {
		name string
		j    Jurisdiction
		want error
	}{
		{"lowercase country", Jurisdiction{Country: "us"}, ErrJurisdictionCountry},
		{"three-letter country", Jurisdiction{Country: "USA"}, ErrJurisdictionCountry},
		{"empty country", Jurisdiction{}, ErrJurisdictionCountry},
		{"single-letter state", Jurisdiction{Country: "US", State: "C"}, ErrJurisdictionState},
		{"digit in state", Jurisdiction{Country: "US", State: "C1"}, ErrJurisdictionState},
		{"four-letter state", Jurisdiction{Country: "US", State: "CALI"}, ErrJurisdictionState},
		{"padded locality", Jurisdiction{Country: "US", State: "CA", Locality: " Oakland"}, ErrJurisdictionLocality},
	}
	for _, c := range cases {
		err := c.j.Validate()
		if !errors.Is(err, c.want) {
			t.Errorf("%s: Validate() = %v, want errors.Is %v", c.name, err, c.want)
		}
	}
}

func TestIsStateResolvedRequiresCountryAndState(t *testing.T) {
	if (Jurisdiction{Country: "US"}).IsStateResolved() {
		t.Error("country-only jurisdiction reported as state resolved")
	}
	if !(Jurisdiction{Country: "US", State: "CA"}).IsStateResolved() {
		t.Error("country+state jurisdiction not reported as state resolved")
	}
	if (Jurisdiction{Country: "US", State: "C"}).IsStateResolved() {
		t.Error("malformed state reported as state resolved")
	}
}

func TestStringAndIsZero(t *testing.T) {
	if got := (Jurisdiction{}).String(); got != "" {
		t.Errorf("zero String() = %q, want empty", got)
	}
	if !(Jurisdiction{}).IsZero() {
		t.Error("zero jurisdiction not IsZero")
	}
	j := Jurisdiction{Country: "US", State: "NY", Locality: "Albany"}
	if got := j.String(); got != "US-NY-Albany" {
		t.Errorf("String() = %q, want US-NY-Albany", got)
	}
	if j.IsZero() {
		t.Error("populated jurisdiction reported IsZero")
	}
}

func TestEqualComparesEveryField(t *testing.T) {
	a := Jurisdiction{Country: "US", State: "NY"}
	if !a.Equal(Jurisdiction{Country: "US", State: "NY"}) {
		t.Error("identical jurisdictions not Equal")
	}
	if a.Equal(Jurisdiction{Country: "US", State: "NY", Locality: "Albany"}) {
		t.Error("jurisdictions differing by locality reported Equal")
	}
}

// TestCanonicalBytesPinsFraming pins the exact bytes: each field is a
// big-endian uint32 label length, the label, a uint32 value length and the
// value, so the framing cannot be reparsed as a different split.
func TestCanonicalBytesPinsFraming(t *testing.T) {
	got := Jurisdiction{Country: "US", State: "CA"}.CanonicalBytes(nil)
	want := []byte{
		0, 0, 0, 7, 'c', 'o', 'u', 'n', 't', 'r', 'y',
		0, 0, 0, 2, 'U', 'S',
		0, 0, 0, 5, 's', 't', 'a', 't', 'e',
		0, 0, 0, 2, 'C', 'A',
		0, 0, 0, 8, 'l', 'o', 'c', 'a', 'l', 'i', 't', 'y',
		0, 0, 0, 0,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("CanonicalBytes = %x\nwant %x", got, want)
	}
}

func TestCanonicalBytesAppendsAndDistinguishesFields(t *testing.T) {
	prefix := []byte("prefix")
	out := Jurisdiction{Country: "US"}.CanonicalBytes(prefix)
	if !bytes.HasPrefix(out, prefix) {
		t.Fatal("CanonicalBytes did not append to dst")
	}
	// A state in the state slot must not encode the same as one in the
	// locality slot.
	a := Jurisdiction{Country: "US", State: "CA"}.CanonicalBytes(nil)
	b := Jurisdiction{Country: "US", Locality: "CA"}.CanonicalBytes(nil)
	if bytes.Equal(a, b) {
		t.Fatal("state and locality encode identically")
	}
}
