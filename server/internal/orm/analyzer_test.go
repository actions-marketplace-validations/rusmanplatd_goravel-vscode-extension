package orm

import (
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

const fixture = "../../testdata/project"

func diagMessages(t *testing.T, src string) []string {
	t.Helper()
	cat := indexer.Build(fixture)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	var out []string
	for _, d := range fa.Diagnostics(cat) {
		out = append(out, d.Message)
	}
	return out
}

func wrap(body string) string {
	return "package repositories\n\nimport \"github.com/rusmanplatd/basegoravel/app/facades\"\n\nfunc f() {\n" + body + "\n}\n"
}

func hasMsg(msgs []string, sub string) bool {
	for _, m := range msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

// ---------- positive: valid code produces no diagnostics ----------

func TestORM_Positive_ValidColumnsNoDiagnostics(t *testing.T) {
	cases := map[string]string{
		"db table where":   `facades.DB().Table("ref_geo_cities").Where("name = ?", "x").Get(nil)`,
		"where and order":  `facades.DB().Table("ref_geo_cities").Where("province_id = ?", 1).Order("name DESC").Get(nil)`,
		"select columns":   `facades.DB().Table("ref_geo_cities").Select("id", "name").Get(nil)`,
		"orm model query":  `facades.Orm().Query().Model(&models.RefGeoCity{}).Where("code = ?", "BDG").Get(nil)`,
		"config known key": `facades.Config().GetString("jwt.secret")`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if msgs := diagMessages(t, wrap(body)); len(msgs) != 0 {
				t.Errorf("expected no diagnostics, got %v", msgs)
			}
		})
	}
}

func TestORM_Positive_SpansDetected(t *testing.T) {
	cat := indexer.Build(fixture)
	src := wrap(`facades.DB().Table("ref_geo_cities").Where("name = ?", "x").Get(nil)`)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	off := strings.Index(src, "name = ?") + 1
	sp := fa.SpanAt(off)
	if sp == nil {
		t.Fatal("expected a span at the Where string literal")
	}
	if sp.Kind != SpanORMWhere {
		t.Errorf("expected SpanORMWhere, got %v", sp.Kind)
	}
	if sp.Table == nil || sp.Table.Name != "ref_geo_cities" {
		t.Errorf("expected span resolved to ref_geo_cities, got %+v", sp.Table)
	}
}

func TestORM_Positive_ConfigKeySpan(t *testing.T) {
	cat := indexer.Build(fixture)
	src := wrap(`facades.Config().GetString("jwt.secret")`)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	sp := fa.SpanAt(strings.Index(src, "jwt.secret") + 2)
	if sp == nil || sp.Kind != SpanConfigKey {
		t.Fatalf("expected SpanConfigKey, got %+v", sp)
	}
}

func TestORM_Positive_HoverOnColumn(t *testing.T) {
	cat := indexer.Build(fixture)
	src := wrap(`facades.DB().Table("ref_geo_cities").Where("name = ?", "x").Get(nil)`)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	md, _, _, ok := fa.Hover(strings.Index(src, "name = ?")+1, cat)
	if !ok || !strings.Contains(md, "name") {
		t.Errorf("expected hover docs for column name, got ok=%v md=%q", ok, md)
	}
}

func TestORM_Positive_CompletionOffersColumns(t *testing.T) {
	cat := indexer.Build(fixture)
	src := wrap(`facades.DB().Table("ref_geo_cities").Where("na", "x").Get(nil)`)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	items, ok := fa.Completion(strings.Index(src, `"na"`)+3, cat)
	if !ok {
		t.Fatal("expected completion context")
	}
	found := false
	for _, it := range items {
		if it["label"] == "name" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected column 'name' among completions, got %v", items)
	}
}

// ---------- negative: invalid code is flagged ----------

func TestORM_Negative_UnknownColumn(t *testing.T) {
	msgs := diagMessages(t, wrap(`facades.DB().Table("ref_geo_cities").Where("nope = ?", 1).Get(nil)`))
	if !hasMsg(msgs, "Column 'nope' does not exist on table 'ref_geo_cities'") {
		t.Errorf("expected unknown-column diagnostic, got %v", msgs)
	}
}

func TestORM_Negative_UnknownColumnOnModelQuery(t *testing.T) {
	msgs := diagMessages(t, wrap(`facades.Orm().Query().Model(&models.RefGeoCity{}).Where("bogus = ?", 1).Get(nil)`))
	if !hasMsg(msgs, "Column 'bogus' does not exist") {
		t.Errorf("expected unknown-column diagnostic, got %v", msgs)
	}
}

func TestORM_Negative_UnknownColumnInSelect(t *testing.T) {
	msgs := diagMessages(t, wrap(`facades.DB().Table("ref_geo_cities").Select("id", "ghost").Get(nil)`))
	if !hasMsg(msgs, "ghost") {
		t.Errorf("expected diagnostic for Select column 'ghost', got %v", msgs)
	}
}

func TestORM_Negative_UnknownColumnInOrder(t *testing.T) {
	msgs := diagMessages(t, wrap(`facades.DB().Table("ref_geo_cities").Order("zzz DESC").Get(nil)`))
	if !hasMsg(msgs, "zzz") {
		t.Errorf("expected diagnostic for Order column 'zzz', got %v", msgs)
	}
}

func TestORM_Negative_UnknownConfigKeyIsWarning(t *testing.T) {
	cat := indexer.Build(fixture)
	src := wrap(`facades.Config().GetString("jwt.missing")`)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	ds := fa.Diagnostics(cat)
	if len(ds) != 1 || ds[0].Severity != 2 || !strings.Contains(ds[0].Message, "Unknown config key 'jwt.missing'") {
		t.Errorf("expected single warning for unknown config key, got %+v", ds)
	}
}

func TestORM_Negative_OnlyOffendingColumnFlagged(t *testing.T) {
	msgs := diagMessages(t, wrap(`facades.DB().Table("ref_geo_cities").Where("name = ?", "x").Where("nope = ?", 1).Get(nil)`))
	if len(msgs) != 1 {
		t.Errorf("expected exactly 1 diagnostic, got %v", msgs)
	}
}

func TestORM_Negative_UnknownTableProducesNoColumnNoise(t *testing.T) {
	msgs := diagMessages(t, wrap(`facades.DB().Table("totally_unknown").Where("anything = ?", 1).Get(nil)`))
	if hasMsg(msgs, "Column 'anything'") {
		t.Errorf("must not flag columns on unresolved tables, got %v", msgs)
	}
}

func TestORM_Negative_NonFrameworkCodeIgnored(t *testing.T) {
	src := "package x\n\nfunc f(m map[string]string) string {\n\treturn m[\"nope = ?\"]\n}\n"
	if msgs := diagMessages(t, src); len(msgs) != 0 {
		t.Errorf("plain Go must yield no diagnostics, got %v", msgs)
	}
}

func TestORM_Negative_NoSpanOutsideLiteral(t *testing.T) {
	cat := indexer.Build(fixture)
	src := wrap(`facades.DB().Table("ref_geo_cities").Where("name = ?", "x").Get(nil)`)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	if sp := fa.SpanAt(0); sp != nil {
		t.Errorf("expected no span at offset 0, got %+v", sp)
	}
}

func TestORM_Negative_SyntaxErrorDoesNotPanic(t *testing.T) {
	cat := indexer.Build(fixture)
	for _, src := range []string{"", "package", "package x\nfunc (", "garbage \x00 bytes"} {
		fa := AnalyzeFile("/proj/app/a.go", []byte(src), cat)
		if fa != nil {
			_ = fa.Diagnostics(cat)
		}
	}
}

func TestORM_Negative_HoverOnUnknownColumnReturnsNothing(t *testing.T) {
	cat := indexer.Build(fixture)
	src := wrap(`facades.DB().Table("ref_geo_cities").Where("nope = ?", "x").Get(nil)`)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	if md, _, _, ok := fa.Hover(strings.Index(src, "nope")+1, cat); ok && strings.Contains(md, "uuid") {
		t.Errorf("unexpected hover for unknown column: %q", md)
	}
}

// ---------- table hover ----------

func hoverAt(t *testing.T, src, needle string, delta int) (string, bool) {
	t.Helper()
	cat := indexer.Build(fixture)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	md, _, _, ok := fa.Hover(strings.Index(src, needle)+delta, cat)
	return md, ok
}

func TestORM_Positive_HoverOnTableName(t *testing.T) {
	src := wrap(`facades.DB().Table("ref_geo_cities").Get(nil)`)
	md, ok := hoverAt(t, src, "ref_geo_cities", 3)
	if !ok {
		t.Fatal("expected hover on table literal")
	}
	for _, want := range []string{"**Table `ref_geo_cities`**", "models.RefGeoCity", "Columns: 12", "`province_id`"} {
		if !strings.Contains(md, want) {
			t.Errorf("hover missing %q:\n%s", want, md)
		}
	}
}

func TestORM_Positive_HoverTableRangeCoversLiteral(t *testing.T) {
	cat := indexer.Build(fixture)
	src := wrap(`facades.DB().Table("ref_geo_cities").Get(nil)`)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	_, s, e, ok := fa.Hover(strings.Index(src, "ref_geo_cities")+1, cat)
	if !ok || src[s:e] != "ref_geo_cities" {
		t.Errorf("range should cover the table name, got %q ok=%v", src[s:e], ok)
	}
}

func TestORM_Positive_HoverTableWithSchemaPrefix(t *testing.T) {
	src := wrap(`facades.DB().Table("geo_app.ref_geo_cities").Get(nil)`)
	md, ok := hoverAt(t, src, "geo_app", 8)
	if !ok || !strings.Contains(md, "ref_geo_cities") {
		t.Errorf("schema-qualified name should resolve, ok=%v md=%q", ok, md)
	}
}

func TestORM_Positive_HoverTableWithAlias(t *testing.T) {
	src := wrap(`facades.DB().Table("ref_geo_cities ct").Get(nil)`)
	md, ok := hoverAt(t, src, "ref_geo_cities", 2)
	if !ok || !strings.Contains(md, "**Table `ref_geo_cities`**") {
		t.Errorf("aliased table should resolve, ok=%v md=%q", ok, md)
	}
}

func TestORM_Positive_HoverTableOnOrmQuery(t *testing.T) {
	src := wrap(`facades.Orm().Query().Table("ref_geo_provinces").Get(nil)`)
	md, ok := hoverAt(t, src, "ref_geo_provinces", 2)
	if !ok || !strings.Contains(md, "ref_geo_provinces") {
		t.Errorf("Query().Table should hover too, ok=%v md=%q", ok, md)
	}
}

func TestORM_Positive_TableHoverDoesNotBreakColumnHover(t *testing.T) {
	src := wrap(`facades.DB().Table("ref_geo_cities").Where("name = ?", "x").Get(nil)`)
	md, ok := hoverAt(t, src, "name = ?", 1)
	if !ok || !strings.Contains(md, "ref_geo_cities.name") {
		t.Errorf("column hover regressed: ok=%v md=%q", ok, md)
	}
}

func TestORM_Negative_HoverUnknownTableReturnsNothing(t *testing.T) {
	src := wrap(`facades.DB().Table("totally_unknown").Get(nil)`)
	if md, ok := hoverAt(t, src, "totally_unknown", 2); ok {
		t.Errorf("unknown table must not hover, got %q", md)
	}
}

func TestORM_Negative_TableWithNonLiteralArgNoSpan(t *testing.T) {
	cat := indexer.Build(fixture)
	src := wrap(`name := "ref_geo_cities"` + "\n\t" + `facades.DB().Table(name).Get(nil)`)
	fa := AnalyzeFile("/proj/app/repositories/a.go", []byte(src), cat)
	for _, sp := range fa.Spans {
		if sp.Kind == SpanORMTable {
			t.Errorf("non-literal argument must not create a table span: %+v", sp)
		}
	}
}

func TestORM_Negative_TableSpanProducesNoDiagnostics(t *testing.T) {
	for _, tbl := range []string{"ref_geo_cities", "totally_unknown"} {
		if msgs := diagMessages(t, wrap(`facades.DB().Table("`+tbl+`").Get(nil)`)); len(msgs) != 0 {
			t.Errorf("Table(%q) alone must not add diagnostics, got %v", tbl, msgs)
		}
	}
}

func TestORM_Negative_HoverOutsideTableLiteral(t *testing.T) {
	src := wrap(`facades.DB().Table("ref_geo_cities").Get(nil)`)
	if md, ok := hoverAt(t, src, "Get(nil)", 1); ok {
		t.Errorf("hover off the literal must be empty, got %q", md)
	}
}
