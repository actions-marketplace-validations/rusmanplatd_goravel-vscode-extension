package orm

import (
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func TestCompletionEscapedStringBounds(t *testing.T) {
	cat := indexer.NewCatalog()
	tbl := &indexer.Table{
		Name: "users",
		Relations: []*indexer.Relation{
			{Name: "Profile", TargetModel: "Profile", TargetTable: "profiles", ForeignKey: "user_id"},
		},
	}

	// Simulate an escaped Go string literal where raw span length (Start..End)
	// is larger than len(sp.Text) after strconv.Unquote.
	fa := &FileAnalysis{
		Spans: []*Span{
			{
				Kind:  SpanORMRelation,
				Start: 10,
				End:   30,
				Text:  "Profile\n\t",
				Table: tbl,
			},
			{
				Kind:  SpanRuleValue,
				Start: 40,
				End:   60,
				Text:  "required|exists:\n",
			},
		},
	}

	for off := 10; off <= 60; off++ {
		fa.Completion(off, cat)
	}
}

func TestStrMaskAndOrderDiagnostics(t *testing.T) {
	masked := strMask(`meta::jsonb ->> 'key' = 'val''ue'`)
	if masked == "" {
		t.Fatal("expected non-empty masked string")
	}
	for _, tok := range []string{"jsonb", "key", "val", "ue"} {
		if containsWord(masked, tok) {
			t.Fatalf("expected %q to be masked in %q", tok, masked)
		}
	}

	cat := indexer.NewCatalog()
	tbl := &indexer.Table{
		Name: "users",
		Columns: []*indexer.Column{
			{Name: "created_at", SQLType: "TIMESTAMP"},
			{Name: "id", SQLType: "BIGINT"},
		},
	}
	fa := &FileAnalysis{
		Spans: []*Span{
			{
				Kind:  SpanORMOrder,
				Start: 0,
				End:   20,
				Text:  "created_at DESC, id ASC",
				Table: tbl,
			},
		},
	}
	if diags := fa.Diagnostics(cat); len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for valid multi-column Order, got %+v", diags)
	}
}

func containsWord(s, w string) bool {
	for _, f := range identRe.FindAllString(s, -1) {
		if f == w {
			return true
		}
	}
	return false
}
