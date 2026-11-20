package clienttelemetry

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const prefix = `{"v":1,"build":"b","locale":"en-US","reason":"error","session":"0123456789abcdef"`

const validBody = `{"v":1,"build":"abc123.d","locale":"de-DE","reason":"pagehide","session":"0123456789abcdef","marks":{"go-main":120,"hydrated":900},"stream":{"open":3,"gave-up":1},"panics":{"history.push":2}}`

func TestIssue3798_DecodeAcceptsTheVersionedRecord(t *testing.T) {
	rec, err := Decode(strings.NewReader(validBody))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if rec.Build != "abc123.d" || rec.Locale != "de-DE" || rec.Reason != ReasonPageHide || rec.Marks["hydrated"] != 900 || rec.Stream["gave-up"] != 1 || rec.Panics["history.push"] != 2 {
		t.Fatalf("record = %+v", rec)
	}
}

func TestIssue3798_DecodeRefusesAnythingOffTheAllowList_Security(t *testing.T) {
	cases := map[string]string{
		"unknown field (message content)": prefix + `,"message":"hello Bob"}`,
		"wrong version":                   `{"v":2,"build":"b","locale":"en-US","reason":"error","session":"0123456789abcdef"}`,
		"unknown reason":                  `{"v":1,"build":"b","locale":"en-US","reason":"click","session":"0123456789abcdef"}`,
		"unknown stream key":              prefix + `,"stream":{"conv_42":1}}`,
		"id-shaped panic site":            prefix + `,"panics":{"Bob Smith <bob@example.com>":1}}`,
		"id-shaped mark name":             prefix + `,"marks":{"conversation 7":1}}`,
		"negative count":                  prefix + `,"stream":{"open":-1}}`,
		"absurd mark":                     prefix + `,"marks":{"dial":99999999999}}`,
		"bad session":                     `{"v":1,"build":"b","locale":"en-US","reason":"error","session":"not hex"}`,
		"bad locale":                      `{"v":1,"build":"b","locale":"<script>","reason":"error","session":"0123456789abcdef"}`,
		"trailing data":                   validBody + ` {}`,
		"not json":                        `hello`,
		"empty":                           ``,
	}
	for name, body := range cases {
		if _, err := Decode(strings.NewReader(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestIssue3798_DecodeBoundsTheBody_Security(t *testing.T) {
	big := prefix + `,"pad":"` + strings.Repeat("x", MaxBodyBytes) + `"}`
	if _, err := Decode(strings.NewReader(big)); err != ErrTooLarge {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	var sb strings.Builder
	sb.WriteString(prefix + `,"panics":{`)
	for i := 0; i < MaxPanicSites+1; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf(`"site%d":1`, i))
	}
	sb.WriteString(`}}`)
	if _, err := Decode(strings.NewReader(sb.String())); err == nil {
		t.Fatal("too many panic sites accepted")
	}
}

func TestIssue3798_SessionHashIsTenantScopedAndOpaque(t *testing.T) {
	a := SessionHash("tenant-a", "0123456789abcdef")
	if a == "" || strings.Contains(a, "tenant") || strings.Contains(a, "0123456789abcdef") {
		t.Fatalf("hash leaks input: %q", a)
	}
	if a != SessionHash("tenant-a", "0123456789abcdef") {
		t.Fatal("hash is not stable")
	}
	if a == SessionHash("tenant-b", "0123456789abcdef") {
		t.Fatal("same session in another tenant must hash differently")
	}
}

func TestIssue3798_LimiterCapsPerSessionAndBoundsItsMemory_Security(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLimiter(3, time.Minute, 2, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if !l.Allow("s1") {
			t.Fatalf("call %d refused", i)
		}
	}
	if l.Allow("s1") {
		t.Fatal("fourth call inside the window accepted")
	}
	if !l.Allow("s2") {
		t.Fatal("another session must not be affected")
	}
	if l.Allow("s3") {
		t.Fatal("a full table must refuse new sessions rather than grow")
	}
	now = now.Add(2 * time.Minute)
	if !l.Allow("s1") || !l.Allow("s3") {
		t.Fatal("window did not reset and free the table")
	}
}

func TestIssue3798_AttrsCarryOnlyAllowListedFacts(t *testing.T) {
	rec, err := Decode(strings.NewReader(validBody))
	if err != nil {
		t.Fatal(err)
	}
	joined := fmt.Sprint(rec.Attrs("hashhash")...)
	for _, want := range []string{"hashhash", "abc123.d", "de-DE", "go-main=120", "history.push=2", "gave-up=1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("attrs missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "0123456789abcdef") {
		t.Error("raw client session nonce reached the log attributes")
	}
}
