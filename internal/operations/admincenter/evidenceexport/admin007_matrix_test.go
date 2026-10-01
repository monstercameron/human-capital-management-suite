package evidenceexport

import (
	"errors"
	"testing"
	"time"
)

// TestADMIN007ValidationMatrix keeps the operator contract exact at its
// boundary.  These are intentionally table-driven: each row is one refusal
// or acceptance that an export adapter must preserve.
func TestADMIN007ValidationMatrix(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	cases := []struct {
		name string
		edit func(*Request)
		want error
	}{
		{"missing tenant", func(r *Request) { r.Authorization.TenantID = "" }, ErrUnauthorized},
		{"wrong purpose", func(r *Request) { r.Authorization.Purpose = "backup" }, ErrUnauthorized},
		{"missing scope", func(r *Request) { r.Authorization.Scope = "" }, ErrUnauthorized},
		{"missing redaction proof", func(r *Request) { r.Authorization.RedactionProfile = "" }, ErrUnauthorized},
		{"missing source proof", func(r *Request) { r.Authorization.SourceProof = "" }, ErrUnauthorized},
		{"missing config proof", func(r *Request) { r.Authorization.ConfigProof = "" }, ErrUnauthorized},
		{"missing policy proof", func(r *Request) { r.Authorization.PolicyProof = "" }, ErrUnauthorized},
		{"expired", func(r *Request) { r.Authorization.ExpiresAt = now }, ErrExpired},
		{"oversized authorization ttl", func(r *Request) { r.Authorization.ExpiresAt = r.Authorization.IssuedAt.Add(MaxTTL + time.Nanosecond) }, ErrUnauthorized},
		{"unauthorized field", func(r *Request) { r.Records[0].Fields["secret"] = "x" }, ErrUnauthorized},
		{"empty query", func(r *Request) { r.Query = "" }, ErrInvalidRequest},
		{"reversed range", func(r *Request) { r.From = r.To }, ErrInvalidRequest},
		{"oversized range", func(r *Request) { r.From = r.To.Add(-MaxTTL - time.Nanosecond) }, ErrInvalidRequest},
		{"zero chunk size", func(r *Request) { r.ChunkSize = 0 }, ErrInvalidRequest},
		{"empty records", func(r *Request) { r.Records = nil }, ErrInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest(now)
			tc.edit(&r)
			if _, err := NewManager().Start(r, now); !errors.Is(err, tc.want) {
				t.Fatalf("Start error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestADMIN007ProgressFailureRecoveryMatrix(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	m := NewManager()
	op, err := m.Start(validRequest(now), now)
	if err != nil {
		t.Fatal(err)
	}
	if got := BuildView(op); got.Status != StatusPending || got.Completed != 0 || got.Total != 3 || got.NextChunk != 0 || !got.CanResume || got.CanRetry {
		t.Fatalf("pending view = %+v", got)
	}
	failed, err := m.Fail(op.ID, "source unavailable")
	if err != nil {
		t.Fatal(err)
	}
	if got := BuildView(failed); got.Status != StatusFailed || got.Failure != "source unavailable" || !got.CanResume || !got.CanRetry || got.Completed != 0 {
		t.Fatalf("failed view = %+v", got)
	}
	recovered, err := m.Resume(op.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := BuildView(recovered); got.Status != StatusComplete || got.Completed != got.Total || got.NextChunk != recovered.Manifest.ChunkCount || got.CanResume || got.CanRetry || got.ManifestDigest == "" {
		t.Fatalf("recovered view = %+v", got)
	}
	if _, err := m.Resume(op.ID, 0, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale post-completion checkpoint error = %v, want %v", err, ErrConflict)
	}
}

func TestADMIN007ImmutableManifestAndOfflineSignatureMatrix(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	m := NewManager()
	op, err := m.Start(validRequest(now), now)
	if err != nil {
		t.Fatal(err)
	}
	op, err = m.Resume(op.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	p := op.Artifact(validRequest(now).Records)
	if !VerifyOffline(p) {
		t.Fatal("valid package failed offline verification")
	}
	cases := []struct {
		name string
		edit func(*Package)
	}{
		{"scope", func(p *Package) { p.Manifest.Scope = "other" }},
		{"query", func(p *Package) { p.Manifest.Query = "other" }},
		{"redaction profile", func(p *Package) { p.Manifest.RedactionProfile = "full" }},
		{"source proof", func(p *Package) { p.Manifest.SourceProof = "forged" }},
		{"config proof", func(p *Package) { p.Manifest.ConfigProof = "forged" }},
		{"policy proof", func(p *Package) { p.Manifest.PolicyProof = "forged" }},
		{"expiry", func(p *Package) { p.Manifest.ExpiresAt = p.Manifest.ExpiresAt.Add(time.Minute) }},
		{"record digest", func(p *Package) { p.Records[0].Digest = "forged" }},
		{"record count", func(p *Package) { p.Records = p.Records[:1] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy := Package{Manifest: p.Manifest, Records: append([]Record(nil), p.Records...)}
			tc.edit(&copy)
			if !errors.Is(copy.Verify(), ErrTampered) {
				t.Fatalf("Verify() = nil for %s", tc.name)
			}
			if VerifyOffline(copy) {
				t.Fatalf("VerifyOffline() accepted %s tampering", tc.name)
			}
		})
	}
}

func TestADMIN007PermutationDeterminismMatrix(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	base := validRequest(now)
	m0 := NewManager()
	op0, err := m0.Start(base, now)
	if err != nil {
		t.Fatal(err)
	}
	op0, err = m0.Resume(op0.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	wantContent := op0.Manifest.ContentDigest
	wantManifest := op0.Manifest.ManifestDigest
	if wantContent == "" || wantManifest == "" {
		t.Fatal("baseline digests empty")
	}
	cases := []struct {
		name  string
		order []int
	}{
		{"sorted", []int{0, 1, 2}},
		{"reverse", []int{2, 1, 0}},
		{"rotate-left", []int{1, 2, 0}},
		{"rotate-right", []int{2, 0, 1}},
		{"swap-first-two", []int{1, 0, 2}},
		{"swap-last-two", []int{0, 2, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest(now)
			perm := make([]Record, len(tc.order))
			for i, idx := range tc.order {
				src := base.Records[idx]
				dst := Record{ID: src.ID, Digest: src.Digest}
				if src.Fields != nil {
					dst.Fields = make(map[string]string, len(src.Fields))
					for k, v := range src.Fields {
						dst.Fields[k] = v
					}
				}
				perm[i] = dst
			}
			req.Records = perm
			m := NewManager()
			op, err := m.Start(req, now)
			if err != nil {
				t.Fatal(err)
			}
			op, err = m.Resume(op.ID, 0, now)
			if err != nil {
				t.Fatal(err)
			}
			if op.Manifest.ContentDigest != wantContent {
				t.Fatalf("ContentDigest = %s, want %s for order %v", op.Manifest.ContentDigest, wantContent, tc.order)
			}
			if op.Manifest.ManifestDigest != wantManifest {
				t.Fatalf("ManifestDigest = %s, want %s for order %v", op.Manifest.ManifestDigest, wantManifest, tc.order)
			}
			if !op.Manifest.VerifyDigest() {
				t.Fatal("manifest digest failed verification")
			}
		})
	}
}

func TestADMIN007CallerCopyMatrix(t *testing.T) {
	sortCases := []struct {
		name  string
		input []Record
	}{
		{"unsorted distinct", []Record{{ID: "c", Digest: "dc"}, {ID: "a", Digest: "da", Fields: map[string]string{"id": "a"}}, {ID: "b", Digest: "db", Fields: map[string]string{"kind": "event"}}}},
		{"equal ids", []Record{{ID: "same", Digest: "db", Fields: map[string]string{"id": "b"}}, {ID: "same", Digest: "da", Fields: map[string]string{"id": "a"}}}},
		{"single", []Record{{ID: "a", Digest: "da", Fields: map[string]string{"id": "a"}}}},
	}
	for _, tc := range sortCases {
		t.Run("sort keeps caller "+tc.name, func(t *testing.T) {
			wantIDs := make([]string, len(tc.input))
			wantDigests := make([]string, len(tc.input))
			wantFields := make([]map[string]string, len(tc.input))
			for i, r := range tc.input {
				wantIDs[i] = r.ID
				wantDigests[i] = r.Digest
				if r.Fields != nil {
					cp := make(map[string]string, len(r.Fields))
					for k, v := range r.Fields {
						cp[k] = v
					}
					wantFields[i] = cp
				}
			}
			out := SortRecords(tc.input)
			if len(out) != len(tc.input) {
				t.Fatalf("SortRecords len = %d, want %d", len(out), len(tc.input))
			}
			for i, r := range tc.input {
				if r.ID != wantIDs[i] || r.Digest != wantDigests[i] {
					t.Fatalf("caller reordered at %d: got %+v want ID %s", i, r, wantIDs[i])
				}
				if len(r.Fields) != len(wantFields[i]) {
					t.Fatalf("caller fields mutated at %d", i)
				}
				for k, v := range wantFields[i] {
					if r.Fields[k] != v {
						t.Fatalf("caller field %q mutated at %d", k, i)
					}
				}
			}
			out[0].ID = "mutated"
			if out[0].Fields == nil {
				out[0].Fields = map[string]string{"id": "mutated"}
			} else {
				for k := range out[0].Fields {
					out[0].Fields[k] = "mutated"
				}
				out[0].Fields["extra"] = "mutated"
			}
			for i, r := range tc.input {
				if r.ID == "mutated" {
					t.Fatalf("returned slice aliases caller at %d", i)
				}
				for k, v := range r.Fields {
					if v == "mutated" {
						t.Fatalf("returned map aliases caller at %d field %q", i, k)
					}
				}
				if _, ok := r.Fields["extra"]; ok {
					t.Fatalf("returned map leaked extra field at %d", i)
				}
			}
		})
	}
	startCases := []struct {
		name  string
		order []int
	}{
		{"reverse", []int{2, 1, 0}},
		{"rotate", []int{1, 2, 0}},
	}
	for _, tc := range startCases {
		t.Run("start keeps caller "+tc.name, func(t *testing.T) {
			now := time.Unix(1000, 0).UTC()
			base := validRequest(now)
			req := validRequest(now)
			perm := make([]Record, len(tc.order))
			for i, idx := range tc.order {
				src := base.Records[idx]
				dst := Record{ID: src.ID, Digest: src.Digest}
				if src.Fields != nil {
					dst.Fields = make(map[string]string, len(src.Fields))
					for k, v := range src.Fields {
						dst.Fields[k] = v
					}
				}
				perm[i] = dst
			}
			req.Records = perm
			wantFirst := req.Records[0].ID
			wantLast := req.Records[len(req.Records)-1].ID
			if _, err := NewManager().Start(req, now); err != nil {
				t.Fatal(err)
			}
			if req.Records[0].ID != wantFirst || req.Records[len(req.Records)-1].ID != wantLast {
				t.Fatalf("Start reordered caller: got [%s..%s] want [%s..%s]", req.Records[0].ID, req.Records[len(req.Records)-1].ID, wantFirst, wantLast)
			}
		})
	}
}

func TestADMIN007EqualIDTieMatrix(t *testing.T) {
	cases := []struct {
		name  string
		input []Record
		want  []Record
	}{
		{
			"digest breaks id tie",
			[]Record{{ID: "same", Digest: "db", Fields: map[string]string{"id": "b"}}, {ID: "same", Digest: "da", Fields: map[string]string{"id": "a"}}},
			[]Record{{ID: "same", Digest: "da", Fields: map[string]string{"id": "a"}}, {ID: "same", Digest: "db", Fields: map[string]string{"id": "b"}}},
		},
		{
			"fields break digest tie",
			[]Record{{ID: "same", Digest: "da", Fields: map[string]string{"id": "z"}}, {ID: "same", Digest: "da", Fields: map[string]string{"id": "a"}}},
			[]Record{{ID: "same", Digest: "da", Fields: map[string]string{"id": "a"}}, {ID: "same", Digest: "da", Fields: map[string]string{"id": "z"}}},
		},
		{
			"ids primary with ties",
			[]Record{{ID: "b", Digest: "db"}, {ID: "same", Digest: "db"}, {ID: "same", Digest: "da"}, {ID: "a", Digest: "da"}},
			[]Record{{ID: "a", Digest: "da"}, {ID: "b", Digest: "db"}, {ID: "same", Digest: "da"}, {ID: "same", Digest: "db"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orders := []struct {
				name  string
				input []Record
			}{
				{"forward", tc.input},
				{"reverse", reverseRecords(tc.input)},
			}
			for _, ord := range orders {
				t.Run(ord.name, func(t *testing.T) {
					got := SortRecords(ord.input)
					if len(got) != len(tc.want) {
						t.Fatalf("SortRecords len = %d, want %d", len(got), len(tc.want))
					}
					for i := range got {
						if got[i].ID != tc.want[i].ID || got[i].Digest != tc.want[i].Digest {
							t.Fatalf("position %d = {%s %s}, want {%s %s}", i, got[i].ID, got[i].Digest, tc.want[i].ID, tc.want[i].Digest)
						}
						if len(got[i].Fields) != len(tc.want[i].Fields) {
							t.Fatalf("position %d fields len = %d, want %d", i, len(got[i].Fields), len(tc.want[i].Fields))
						}
						for k, v := range tc.want[i].Fields {
							if got[i].Fields[k] != v {
								t.Fatalf("position %d field %q = %q, want %q", i, k, got[i].Fields[k], v)
							}
						}
					}
				})
			}
		})
	}
}

func TestADMIN007FieldsKeyDelimiterCollisionMatrix(t *testing.T) {
	a := map[string]string{"a": "b", "c": "d"}
	b := map[string]string{"a": "b\x01c\x00d"}
	ka, kb := fieldsKey(a), fieldsKey(b)
	if ka == "" || kb == "" {
		t.Fatalf("canonical keys must be non-empty for non-empty maps: %q %q", ka, kb)
	}
	if ka == kb {
		t.Fatalf("canonical keys collide: %q == %q", ka, kb)
	}
}

func TestADMIN007EqualIDEqualDigestFieldsPermutationMatrix(t *testing.T) {
	forward := []Record{
		{ID: "same", Digest: "da", Fields: map[string]string{"a": "b", "c": "d"}},
		{ID: "same", Digest: "da", Fields: map[string]string{"a": "b\x01c\x00d"}},
	}
	reverse := []Record{
		{ID: "same", Digest: "da", Fields: map[string]string{"a": "b\x01c\x00d"}},
		{ID: "same", Digest: "da", Fields: map[string]string{"a": "b", "c": "d"}},
	}
	if fieldsKey(forward[0].Fields) == fieldsKey(forward[1].Fields) {
		t.Fatal("canonical keys must differ for the delimiter collision pair")
	}
	gotFwd := SortRecords(forward)
	gotRev := SortRecords(reverse)
	if len(gotFwd) != 2 || len(gotRev) != 2 {
		t.Fatalf("SortRecords len = %d/%d, want 2/2", len(gotFwd), len(gotRev))
	}
	for i := range gotFwd {
		if gotFwd[i].ID != gotRev[i].ID || gotFwd[i].Digest != gotRev[i].Digest {
			t.Fatalf("permutation %d = {%s %s}, want {%s %s}", i, gotRev[i].ID, gotRev[i].Digest, gotFwd[i].ID, gotFwd[i].Digest)
		}
		if len(gotFwd[i].Fields) != len(gotRev[i].Fields) {
			t.Fatalf("permutation %d fields len = %d, want %d", i, len(gotRev[i].Fields), len(gotFwd[i].Fields))
		}
		for k, v := range gotFwd[i].Fields {
			if gotRev[i].Fields[k] != v {
				t.Fatalf("permutation %d field %q = %q, want %q", i, k, gotRev[i].Fields[k], v)
			}
		}
	}
	firstKey := fieldsKey(forward[0].Fields)
	secondKey := fieldsKey(forward[1].Fields)
	wantFirst := forward[0].Fields
	if secondKey < firstKey {
		wantFirst = forward[1].Fields
	}
	if len(gotFwd[0].Fields) != len(wantFirst) {
		t.Fatalf("tie-break winner fields len = %d, want %d", len(gotFwd[0].Fields), len(wantFirst))
	}
	for k, v := range wantFirst {
		if gotFwd[0].Fields[k] != v {
			t.Fatalf("tie-break winner field %q = %q, want %q", k, gotFwd[0].Fields[k], v)
		}
	}
}

func TestADMIN007DigestContentPermutationMatrix(t *testing.T) {
	low := Record{ID: "a", Digest: "dz", Fields: map[string]string{"id": "a"}}
	midA := Record{ID: "same", Digest: "da", Fields: map[string]string{"a": "b", "c": "d"}}
	midB := Record{ID: "same", Digest: "da", Fields: map[string]string{"a": "b\x01c\x00d"}}
	base := []Record{low, midA, midB}
	want := SortRecords(base)
	perms := [][]Record{
		{low, midA, midB},
		{midB, midA, low},
		{midA, low, midB},
		{midB, low, midA},
	}
	for pi, p := range perms {
		cp := make([]Record, len(p))
		for i, r := range p {
			dst := Record{ID: r.ID, Digest: r.Digest}
			if r.Fields != nil {
				dst.Fields = make(map[string]string, len(r.Fields))
				for k, v := range r.Fields {
					dst.Fields[k] = v
				}
			}
			cp[i] = dst
		}
		got := SortRecords(cp)
		if len(got) != len(want) {
			t.Fatalf("perm %d len = %d, want %d", pi, len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID || got[i].Digest != want[i].Digest {
				t.Fatalf("perm %d position %d = {%s %s}, want {%s %s}", pi, i, got[i].ID, got[i].Digest, want[i].ID, want[i].Digest)
			}
			if len(got[i].Fields) != len(want[i].Fields) {
				t.Fatalf("perm %d position %d fields len = %d, want %d", pi, i, len(got[i].Fields), len(want[i].Fields))
			}
			for k, v := range want[i].Fields {
				if got[i].Fields[k] != v {
					t.Fatalf("perm %d position %d field %q = %q, want %q", pi, i, k, got[i].Fields[k], v)
				}
			}
		}
	}
}

func reverseRecords(in []Record) []Record {
	out := make([]Record, len(in))
	for i, r := range in {
		src := r
		if r.Fields != nil {
			cp := make(map[string]string, len(r.Fields))
			for k, v := range r.Fields {
				cp[k] = v
			}
			src.Fields = cp
		}
		out[len(in)-1-i] = src
	}
	return out
}

func TestADMIN007ChunkSortMatrix(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	wantContent := hash("dc" + hash("db"+hash("da"+"")))
	cases := []struct {
		name       string
		chunkSize  int
		order      []int
		wantChunks int
	}{
		{"size1 reverse", 1, []int{2, 1, 0}, 3},
		{"size2 reverse", 2, []int{2, 1, 0}, 2},
		{"size2 rotate", 2, []int{1, 2, 0}, 2},
		{"size3 reverse", 3, []int{2, 1, 0}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := validRequest(now)
			req := validRequest(now)
			req.ChunkSize = tc.chunkSize
			perm := make([]Record, len(tc.order))
			for i, idx := range tc.order {
				src := base.Records[idx]
				dst := Record{ID: src.ID, Digest: src.Digest}
				if src.Fields != nil {
					dst.Fields = make(map[string]string, len(src.Fields))
					for k, v := range src.Fields {
						dst.Fields[k] = v
					}
				}
				perm[i] = dst
			}
			req.Records = perm
			m := NewManager()
			op, err := m.Start(req, now)
			if err != nil {
				t.Fatal(err)
			}
			if op.Manifest.RecordCount != 3 || op.Total != 3 {
				t.Fatalf("RecordCount/Total = %d/%d, want 3/3", op.Manifest.RecordCount, op.Total)
			}
			if op.Manifest.ChunkCount != tc.wantChunks {
				t.Fatalf("ChunkCount = %d, want %d", op.Manifest.ChunkCount, tc.wantChunks)
			}
			op, err = m.Resume(op.ID, 0, now)
			if err != nil {
				t.Fatal(err)
			}
			if op.Manifest.ContentDigest != wantContent {
				t.Fatalf("ContentDigest = %s, want sorted %s", op.Manifest.ContentDigest, wantContent)
			}
			if !op.Manifest.VerifyDigest() {
				t.Fatal("manifest digest failed verification")
			}
		})
	}
}

func TestADMIN007PackageTamperMatrix(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	m := NewManager()
	op, err := m.Start(validRequest(now), now)
	if err != nil {
		t.Fatal(err)
	}
	op, err = m.Resume(op.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	base := op.Artifact(SortRecords(validRequest(now).Records))
	if err := base.Verify(); err != nil {
		t.Fatalf("valid package Verify = %v", err)
	}
	if !VerifyOffline(base) {
		t.Fatal("valid package failed offline verification")
	}
	cases := []struct {
		name string
		edit func(*Package)
	}{
		{"tenant", func(p *Package) { p.Manifest.TenantID = "other" }},
		{"purpose", func(p *Package) { p.Manifest.Purpose = "backup" }},
		{"from", func(p *Package) { p.Manifest.From = p.Manifest.From.Add(-time.Hour) }},
		{"to", func(p *Package) { p.Manifest.To = p.Manifest.To.Add(time.Hour) }},
		{"allowed fields", func(p *Package) {
			p.Manifest.AllowedFields = append(append([]string(nil), p.Manifest.AllowedFields...), "ssn")
		}},
		{"chunk count", func(p *Package) { p.Manifest.ChunkCount++ }},
		{"content digest", func(p *Package) { p.Manifest.ContentDigest = "forged" }},
		{"manifest digest", func(p *Package) { p.Manifest.ManifestDigest = "forged" }},
		{"record reorder", func(p *Package) { p.Records[0], p.Records[1] = p.Records[1], p.Records[0] }},
		{"record digest", func(p *Package) { p.Records[0].Digest = "forged" }},
		{"record count", func(p *Package) { p.Records = p.Records[:1] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := Package{Manifest: base.Manifest, Records: append([]Record(nil), base.Records...)}
			cp.Manifest.AllowedFields = append([]string(nil), base.Manifest.AllowedFields...)
			tc.edit(&cp)
			if !errors.Is(cp.Verify(), ErrTampered) {
				t.Fatalf("Verify() accepted %s tampering", tc.name)
			}
			if VerifyOffline(cp) {
				t.Fatalf("VerifyOffline() accepted %s tampering", tc.name)
			}
		})
	}
}
