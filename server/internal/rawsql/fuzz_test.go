package rawsql

import (
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func FuzzCheckSQLSyntax(f *testing.F) {
	seeds := []string{
		"SELECT id, name FROM users WHERE email = 'a''b'",
		"SELECT $$dollar 'quoted'$$ FROM users -- comment\n/* block */",
		"SELECT E'escaped \\' quote' FROM users",
		"SELECT (id, FROM users WHERE AND OR",
		"WITH a AS (SELECT 1) SELECT * FROM a",
		"",
		"'",
		`"`,
		"(((",
		")))",
	}
	for _, s := range seeds {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, sql string, dynamic bool) {
		issues := checkSQLSyntax(sql, dynamic)
		for _, is := range issues {
			if is.VStart < 0 || is.VEnd < is.VStart || is.VEnd > len(sql) {
				t.Fatalf("out-of-bounds syntax issue [%d,%d] for len=%d sql=%q", is.VStart, is.VEnd, len(sql), sql)
			}
		}
	})
}

func FuzzParseSelectList(f *testing.F) {
	seeds := []string{
		"SELECT id, u.email AS mail, COUNT(*) cnt FROM users u",
		"WITH c AS (SELECT id FROM users) SELECT DISTINCT * FROM c",
		"SELECT 'a, b' AS lit, func(1, (2, 3)) out FROM t",
		"",
		"WITH RECURSIVE",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, sql string) {
		cols, _ := ParseSelectList(sql)
		for _, c := range cols {
			if c.VStart < 0 || c.VEnd < c.VStart || c.VEnd > len(sql) {
				t.Fatalf("out-of-bounds OutCol [%d,%d] for len=%d sql=%q", c.VStart, c.VEnd, len(sql), sql)
			}
		}
	})
}

func FuzzExtractAndAnalyze(f *testing.F) {
	cat := indexer.NewCatalog()
	seeds := []string{
		"package p\nfunc f() { q := `SELECT id FROM users`; _ = q }",
		"package p\nfunc f() { q := fmt.Sprintf(\"SELECT %s FROM %s WHERE id = $%d\", \"a\", \"b\", 1); _ = q }",
		"package p\nfunc (",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		fi := Extract("/fuzz/a.go", []byte(src), cat)
		if fi == nil {
			return
		}
		for _, d := range Analyze(fi, cat) {
			if d.Start < 0 || d.End < d.Start || d.End > len(src) {
				t.Fatalf("out-of-bounds diag [%d,%d] for len=%d", d.Start, d.End, len(src))
			}
		}
		_ = SemanticTokens(fi, cat)
	})
}
