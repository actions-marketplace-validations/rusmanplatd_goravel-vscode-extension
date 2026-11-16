package framework

import (
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

const fixture = "../../testdata/project"

func requestSrc(rules string) string {
	return `package requests

type CreateCityRequest struct {
	Name       string ` + "`json:\"name\" form:\"name\"`" + `
	ProvinceID string ` + "`json:\"province_id\" form:\"province_id\"`" + `
}

func (r *CreateCityRequest) Rules() map[string]any {
	return map[string]any{
` + rules + `
	}
}
`
}

const reqPath = "/proj/app/http/requests/city_request.go"

// ---------- positive ----------

func TestRequests_Positive_ValidKeysNoDiagnostics(t *testing.T) {
	cat := indexer.Build(fixture)
	src := requestSrc(`"name": "required|string", "province_id": "required|uuid",`)
	if ds := RequestDiagnostics(reqPath, src, cat); len(ds) != 0 {
		t.Errorf("expected no diagnostics, got %+v", ds)
	}
}

func TestRequests_Positive_NestedKeyUsesBaseField(t *testing.T) {
	cat := indexer.Build(fixture)
	src := requestSrc(`"name.first": "required",`)
	if ds := RequestDiagnostics(reqPath, src, cat); len(ds) != 0 {
		t.Errorf("dotted key with valid base should pass, got %+v", ds)
	}
}

func TestRequests_Positive_FixtureFileIsClean(t *testing.T) {
	cat := indexer.Build(fixture)
	p := "/proj/app/http/requests/city_request.go"
	src := requestSrc(`"name": "required|alpha_dash_custom|max_len:255", "province_id": "required|uuid",`)
	if ds := RequestDiagnostics(p, src, cat); len(ds) != 0 {
		t.Errorf("unexpected: %+v", ds)
	}
}

func TestRequests_Positive_CompletionsOfferFields(t *testing.T) {
	cat := indexer.Build(fixture)
	src := requestSrc(`"na": "required",`)
	off := strings.Index(src, `"na"`) + 3
	items := RequestCompletions(reqPath, src, cat, off)
	found := false
	for _, it := range items {
		if it.Label == "name" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'name' completion, got %+v", items)
	}
}

// ---------- negative ----------

func TestRequests_Negative_UnknownKeyWarns(t *testing.T) {
	cat := indexer.Build(fixture)
	src := requestSrc(`"name": "required", "emial": "required",`)
	ds := RequestDiagnostics(reqPath, src, cat)
	if len(ds) != 1 {
		t.Fatalf("expected 1 diagnostic, got %+v", ds)
	}
	if ds[0].Severity != 2 || !strings.Contains(ds[0].Message, "Key 'emial' does not match any field on CreateCityRequest") {
		t.Errorf("unexpected diagnostic %+v", ds[0])
	}
	if got := src[ds[0].Start:ds[0].End]; got != `"emial"` {
		t.Errorf("diagnostic range should cover the key literal, got %s", got)
	}
}

func TestRequests_Negative_UnknownNestedBaseKey(t *testing.T) {
	cat := indexer.Build(fixture)
	src := requestSrc(`"ghost.items.*": "required",`)
	ds := RequestDiagnostics(reqPath, src, cat)
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "'ghost'") {
		t.Errorf("expected diagnostic for base 'ghost', got %+v", ds)
	}
}

func TestRequests_Negative_OutsideRequestsDirIgnored(t *testing.T) {
	cat := indexer.Build(fixture)
	src := requestSrc(`"emial": "required",`)
	if ds := RequestDiagnostics("/proj/app/services/city.go", src, cat); ds != nil {
		t.Errorf("files outside /requests/ must be ignored, got %+v", ds)
	}
}

func TestRequests_Negative_UnknownReceiverIgnored(t *testing.T) {
	cat := indexer.Build(fixture)
	src := strings.ReplaceAll(requestSrc(`"emial": "required",`), "CreateCityRequest", "UnknownRequest")
	if ds := RequestDiagnostics(reqPath, src, cat); len(ds) != 0 {
		t.Errorf("unknown struct must not be checked, got %+v", ds)
	}
}

func TestRequests_Negative_NonRequestMethodIgnored(t *testing.T) {
	cat := indexer.Build(fixture)
	src := strings.Replace(requestSrc(`"emial": "required",`), "Rules()", "Other()", 1)
	if ds := RequestDiagnostics(reqPath, src, cat); len(ds) != 0 {
		t.Errorf("only Rules/Messages/Attributes are checked, got %+v", ds)
	}
}

func TestRequests_Negative_SyntaxErrorReturnsNil(t *testing.T) {
	cat := indexer.Build(fixture)
	if ds := RequestDiagnostics(reqPath, "package requests\nfunc (", cat); ds != nil {
		t.Errorf("syntax error should yield nil, got %+v", ds)
	}
}

func TestRequests_Negative_CompletionOutsideKeyReturnsNothing(t *testing.T) {
	cat := indexer.Build(fixture)
	src := requestSrc(`"name": "required",`)
	items := RequestCompletions(reqPath, src, cat, 0)
	if len(items) != 0 {
		t.Errorf("expected no completions at file start, got %+v", items)
	}
}
