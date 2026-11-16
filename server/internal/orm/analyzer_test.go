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
