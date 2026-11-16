package rawsql

import (
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func starSrc(sql string) string {
	return "package x\n\nfunc f() {\n\tq := `" + sql + "`\n\t_ = q\n}\n"
}

func TestExpandStar_Positive(t *testing.T) {
	cat := indexer.Build(fixture)
	src := starSrc("SELECT * FROM geo_app.ref_geo_provinces")
	fi := Extract("/x/a.go", []byte(src), cat)
	off := strings.Index(src, "*")
	start, end, text, ok := ExpandStar(fi, cat, off)
	if !ok {
		t.Fatal("expected star expansion")
	}
	for _, c := range []string{"id", "name", "country_id"} {
		if !strings.Contains(text, c) {
			t.Errorf("expansion missing %q: %q", c, text)
		}
	}
	if strings.Contains(text, "*") || start > end {
		t.Errorf("bad expansion %q [%d,%d]", text, start, end)
	}
}

func TestExpandStar_Negative_OffsetNotOnStar(t *testing.T) {
	cat := indexer.Build(fixture)
	src := starSrc("SELECT * FROM geo_app.ref_geo_provinces")
	fi := Extract("/x/a.go", []byte(src), cat)
	if _, _, _, ok := ExpandStar(fi, cat, 0); ok {
		t.Error("offset outside SQL must not expand")
	}
}

func TestExpandStar_Negative_UnknownTable(t *testing.T) {
	cat := indexer.Build(fixture)
	src := starSrc("SELECT * FROM geo_app.ghost_table")
	fi := Extract("/x/a.go", []byte(src), cat)
	if _, _, text, ok := ExpandStar(fi, cat, strings.Index(src, "*")); ok {
		t.Errorf("unknown table must not expand, got %q", text)
	}
}
