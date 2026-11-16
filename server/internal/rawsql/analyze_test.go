package rawsql

import (
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

const fixture = "../../testdata/project"

func analyze(t *testing.T, body string) []string {
	t.Helper()
	cat := indexer.Build(fixture)
	src := "package repositories\n\nimport \"github.com/rusmanplatd/basegoravel/app/facades\"\n\nfunc f() {\n" + body + "\n}\n"
	fi := Extract("/proj/app/repositories/a.go", []byte(src), cat)
	var out []string
	for _, d := range Analyze(fi, cat) {
		out = append(out, d.Message)
	}
	return out
}

func contains(msgs []string, sub string) bool {
	for _, m := range msgs {
		if strings.Contains(strings.ToLower(m), strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// ---------- positive ----------

func TestRawSQL_Positive_ValidSelectNoDiagnostics(t *testing.T) {
	cases := map[string]string{
		"simple select":  "q := `SELECT id, name FROM geo_app.ref_geo_cities WHERE deleted_at IS NULL`\n_ = q",
		"aliased select": "q := `SELECT ct.id, ct.name FROM geo_app.ref_geo_cities ct WHERE ct.code = 'X'`\n_ = q",
		"join":           "q := `SELECT ct.name, p.name FROM geo_app.ref_geo_cities ct JOIN geo_app.ref_geo_provinces p ON ct.province_id = p.id`\n_ = q",
		"non sql string": `msg := "hello world"` + "\n_ = msg",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if msgs := analyze(t, body); len(msgs) != 0 {
				t.Errorf("expected none, got %v", msgs)
			}
		})
	}
}

func TestRawSQL_Positive_ExtractFindsSQLDocs(t *testing.T) {
	cat := indexer.Build(fixture)
	src := "package x\n\nfunc f() {\n\tq := `SELECT id FROM geo_app.ref_geo_cities`\n\t_ = q\n}\n"
	fi := Extract("/x/a.go", []byte(src), cat)
	if len(fi.Docs) == 0 {
		t.Fatal("expected at least one SQL doc")
	}
}

func TestRawSQL_Positive_ParseSelectList(t *testing.T) {
	cols, star := ParseSelectList("SELECT id, name AS n, ct.code FROM t")
	if star || len(cols) != 3 {
		t.Fatalf("want 3 cols no star, got %d star=%v", len(cols), star)
	}
	_, star = ParseSelectList("SELECT * FROM t")
	if !star {
		t.Error("expected star for '*'")
	}
}

func TestRawSQL_Positive_Pascal(t *testing.T) {
	if got := Pascal("province_id"); got != "ProvinceId" && got != "ProvinceID" {
		t.Errorf("unexpected Pascal result %q", got)
	}
}

// ---------- negative ----------

func TestRawSQL_Negative_UnknownColumn(t *testing.T) {
	msgs := analyze(t, "q := `SELECT id, ghost FROM geo_app.ref_geo_cities`\n_ = q")
	if !contains(msgs, "ghost") {
		t.Errorf("expected unknown column diagnostic, got %v", msgs)
	}
}

func TestRawSQL_Negative_UnknownColumnInWhere(t *testing.T) {
	msgs := analyze(t, "q := `SELECT id FROM geo_app.ref_geo_cities WHERE nope = 1`\n_ = q")
	if !contains(msgs, "nope") {
		t.Errorf("expected unknown column diagnostic, got %v", msgs)
	}
}

func TestRawSQL_Negative_UnknownTable(t *testing.T) {
	msgs := analyze(t, "q := `SELECT id FROM geo_app.no_such_table`\n_ = q")
	if !contains(msgs, "no_such_table") {
		t.Errorf("expected unknown table diagnostic, got %v", msgs)
	}
}

func TestRawSQL_Negative_PlaceholderArgMismatch(t *testing.T) {
	msgs := analyze(t, "facades.DB().Statement(`DELETE FROM geo_app.ref_geo_cities WHERE name = $1 AND code = $2`, \"a\")")
	if !contains(msgs, "2 placeholder(s) but 1 argument(s)") {
		t.Errorf("expected placeholder mismatch diagnostic, got %v", msgs)
	}
}

func TestRawSQL_Positive_PlaceholderArgMatch(t *testing.T) {
	msgs := analyze(t, "facades.DB().Statement(`DELETE FROM geo_app.ref_geo_cities WHERE name = $1 AND code = $2`, \"a\", \"b\")")
	if contains(msgs, "placeholder") {
		t.Errorf("unexpected placeholder diagnostic: %v", msgs)
	}
}

func TestRawSQL_Negative_NonSQLStringsIgnored(t *testing.T) {
	msgs := analyze(t, `s := "select a thing from the menu"`+"\n_ = s")
	if len(msgs) != 0 {
		t.Errorf("prose must not be treated as SQL, got %v", msgs)
	}
}

func TestRawSQL_Negative_BrokenGoDoesNotPanic(t *testing.T) {
	cat := indexer.Build(fixture)
	for _, src := range []string{"", "package", "package x\nfunc f() { q := `SELECT"} {
		fi := Extract("/x/a.go", []byte(src), cat)
		if fi != nil {
			_ = Analyze(fi, cat)
		}
	}
}

func TestRawSQL_Negative_ParseSelectListEmpty(t *testing.T) {
	cols, star := ParseSelectList("")
	if star || len(cols) != 0 {
		t.Errorf("empty select list should be empty, got %d star=%v", len(cols), star)
	}
}
