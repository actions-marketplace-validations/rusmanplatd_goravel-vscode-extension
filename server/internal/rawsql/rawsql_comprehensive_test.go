package rawsql

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func buildRichCat(t *testing.T) (string, *indexer.Catalog) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"app/models/user.go": `package models
type User struct {
	ID    uint    ` + "`gorm:\"column:id\"`" + `
	Email string  ` + "`gorm:\"column:email\"`" + `
	Bio   *string ` + "`gorm:\"column:bio\"`" + `
}
func (User) TableName() string { return "users" }

type Order struct {
	ID     uint    ` + "`gorm:\"column:id\"`" + `
	UserID uint    ` + "`gorm:\"column:user_id\"`" + `
	Total  float64 ` + "`gorm:\"column:total\"`" + `
	User   *User   ` + "`gorm:\"foreignKey:UserID;references:ID\"`" + `
}
func (Order) TableName() string { return "orders" }

type UserOrderRow struct {
	Email string ` + "`db:\"email\"`" + `
	Bio   string ` + "`db:\"bio\"`" + `
}
`,
		"database/migrations/20250101_create_users.go": `package migrations
func Up() error {
	return facades.Schema().Create("users", func(table schema.Blueprint) {
		table.ID()
		table.String("email")
		table.Text("bio").Nullable()
		table.Index("email")
	})
}
`,
		"database/migrations/20250102_create_orders.go": `package migrations
func Up() error {
	return facades.Schema().Create("orders", func(table schema.Blueprint) {
		table.ID()
		table.BigInteger("user_id")
		table.Decimal("total")
		table.Foreign("user_id").References("id").On("users")
		table.Index("user_id")
	})
}
`,
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, indexer.Build(root)
}

func TestRawSQL_SyntaxValidationAndComments(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		dynamic   bool
		wantSub   string
		wantClean bool
	}{
		{
			name:      "valid multiline with line and block comments",
			sql:       "SELECT id, /* comment with 'unclosed quote */ name -- line 'quote\nFROM users WHERE email = 'a''b' AND bio = E'it\\'s ok'",
			wantClean: true,
		},
		{
			name:      "valid dollar-quoted string",
			sql:       "SELECT $$hello 'world'$$, $tag$body$$inner$tag$ FROM users",
			wantClean: true,
		},
		{
			name:    "unclosed single quote",
			sql:     "SELECT * FROM users WHERE email = 'oops",
			wantSub: "unclosed string literal",
		},
		{
			name:    "unclosed dollar quote",
			sql:     "SELECT $abc$unterminated FROM users",
			wantSub: "unclosed string literal",
		},
		{
			name:    "unclosed double-quoted identifier",
			sql:     `SELECT "email FROM users`,
			wantSub: "unclosed quoted identifier",
		},
		{
			name:    "unexpected closing paren",
			sql:     "SELECT id) FROM users",
			wantSub: "unexpected closing parenthesis",
		},
		{
			name:    "unclosed opening paren",
			sql:     "SELECT (id FROM users",
			wantSub: "unclosed parenthesis",
		},
		{
			name:      "dynamic query skips unclosed paren and trailing clause",
			sql:       "SELECT id FROM users WHERE",
			dynamic:   true,
			wantClean: true,
		},
		{
			name:    "dangling comma before FROM",
			sql:     "SELECT id, FROM users",
			wantSub: "dangling comma",
		},
		{
			name:    "consecutive commas",
			sql:     "SELECT id,, email FROM users",
			wantSub: "consecutive commas",
		},
		{
			name:    "leading comma after SELECT",
			sql:     "SELECT , id FROM users",
			wantSub: "unexpected comma",
		},
		{
			name:    "consecutive logical operators",
			sql:     "SELECT id FROM users WHERE id = 1 AND OR email = 'a'",
			wantSub: "consecutive logical operators",
		},
		{
			name:    "empty SELECT list",
			sql:     "SELECT FROM users",
			wantSub: "empty SELECT list",
		},
		{
			name:    "incomplete trailing WHERE clause",
			sql:     "SELECT id FROM users WHERE ",
			wantSub: "incomplete WHERE clause",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issues := checkSQLSyntax(tc.sql, tc.dynamic)
			if tc.wantClean {
				if len(issues) != 0 {
					t.Errorf("expected no syntax issues, got %+v", issues)
				}
				return
			}
			found := false
			for _, is := range issues {
				if strings.Contains(is.Message, tc.wantSub) {
					found = true
					if is.Severity != 1 {
						t.Errorf("syntax error must be severity 1, got %d", is.Severity)
					}
				}
			}
			if !found {
				t.Errorf("expected issue containing %q, got %+v", tc.wantSub, issues)
			}
		})
	}
}

func TestRawSQL_CTEsSubqueriesFalseKeywordsAndIndexHints(t *testing.T) {
	_, cat := buildRichCat(t)

	t.Run("valid CTE and nested subquery", func(t *testing.T) {
		src := `package repo
func f() {
	q := ` + "`" + `
		WITH active_users AS (
			SELECT id, email AS user_email FROM users
		)
		SELECT au.id, au.user_email, sub.total_amt
		FROM active_users au
		JOIN (SELECT user_id, total AS total_amt FROM orders) sub ON sub.user_id = au.id
		ORDER BY au.user_email
	` + "`" + `
	_ = q
}`
		fi := Extract("/proj/repo.go", []byte(src), cat)
		ds := Analyze(fi, cat)
		for _, d := range ds {
			if d.Severity == 1 {
				t.Errorf("unexpected error in valid CTE/subquery: %+v", d)
			}
		}
	})

	t.Run("unknown column inside CTE body and outer query", func(t *testing.T) {
		src := `package repo
func f() {
	q := ` + "`" + `
		WITH active_users AS (
			SELECT id, emial FROM users
		)
		SELECT id, ghost_col FROM active_users
	` + "`" + `
	_ = q
}`
		fi := Extract("/proj/repo.go", []byte(src), cat)
		ds := Analyze(fi, cat)
		var msgs []string
		for _, d := range ds {
			msgs = append(msgs, d.Message)
		}
		joined := strings.Join(msgs, " | ")
		if !strings.Contains(joined, "emial") || !strings.Contains(joined, "did you mean 'email'") {
			t.Errorf("expected typo suggestion for emial in CTE body, got %v", msgs)
		}
		if !strings.Contains(joined, "ghost_col") {
			t.Errorf("expected unknown column ghost_col in main query, got %v", msgs)
		}
	})

	t.Run("EXTRACT/TRIM/FOR UPDATE/ON CONFLICT DO UPDATE and regression for subquery inside TRIM", func(t *testing.T) {
		// Regression test for Bug #2: FROM users inside TRIM((SELECT ... FROM users)) MUST still resolve `users`!
		src := `package repo
func f() {
	q1 := ` + "`" + `SELECT EXTRACT(YEAR FROM NOW()), TRIM(BOTH ' ' FROM u.email) FROM users u WHERE u.email IS DISTINCT FROM 'x' FOR UPDATE` + "`" + `
	q2 := ` + "`" + `INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO UPDATE SET bio = EXCLUDED.bio` + "`" + `
	q3 := ` + "`" + `SELECT TRIM(COALESCE((SELECT ghost FROM users), '')) FROM orders` + "`" + `
	_ = q1; _ = q2; _ = q3
}`
		fi := Extract("/proj/repo.go", []byte(src), cat)
		ds := Analyze(fi, cat)
		var errs []string
		for _, d := range ds {
			if d.Severity == 1 {
				errs = append(errs, d.Message)
			}
		}
		joined := strings.Join(errs, " | ")
		if strings.Contains(joined, "Unknown table") {
			t.Errorf("EXTRACT/TRIM/DISTINCT/DO UPDATE must not trigger Unknown table: %v", errs)
		}
		if !strings.Contains(joined, "ghost") {
			t.Errorf("regression: subquery inside TRIM must still check table columns, got %v", errs)
		}
	})

	t.Run("system tables, table-valued functions, and DDL skipped", func(t *testing.T) {
		src := `package repo
func f() {
	q1 := ` + "`SELECT relname FROM pg_catalog.pg_class`" + `
	q2 := ` + "`SELECT x FROM unnest(ARRAY[1,2]) AS x`" + `
	q3 := ` + "`CREATE TABLE tmp_x (id INT)`" + `
	_ = q1; _ = q2; _ = q3
}`
		fi := Extract("/proj/repo.go", []byte(src), cat)
		if ds := Analyze(fi, cat); len(ds) != 0 {
			t.Errorf("system tables/TVF/DDL should yield no diagnostics, got %+v", ds)
		}
	})

	t.Run("index-aware hint in WHERE on unindexed column vs indexed column", func(t *testing.T) {
		src := `package repo
func f() {
	q := ` + "`SELECT u.id, u.bio FROM users u WHERE u.bio = 'hi' AND u.email = 'a@b.c'`" + `
	_ = q
}`
		fi := Extract("/proj/repo.go", []byte(src), cat)
		ds := Analyze(fi, cat)
		foundHint := false
		for _, d := range ds {
			if d.Severity == 4 && strings.Contains(d.Message, "'bio'") {
				foundHint = true
			}
			if strings.Contains(d.Message, "'email'") {
				t.Errorf("indexed column 'email' must not trigger hint: %+v", d)
			}
		}
		if !foundHint {
			t.Errorf("expected severity 4 unindexed filter hint on u.bio, got %+v", ds)
		}
	})
}

func TestRawSQL_ExtractionSprintfConcatVarRefsAndFragmentInheritance(t *testing.T) {
	_, cat := buildRichCat(t)
	src := `package repo
import "fmt"
func f() {
	userTbl := (models.User{}).TableName()
	q1 := "SELECT u.id, u.email FROM " + userTbl + " u\n"
	frag := "WHERE u.nope = $1"
	q2 := fmt.Sprintf("SELECT id, %s FROM %s WHERE email = $%d AND bio = $%d AND label = '100%%'", "email", userTbl, 1, 2)
	q3 := fmt.Sprintf("SELECT id FROM %s WHERE id = $1 AND %s = 'x'", userTbl, "email")
	_ = q1; _ = frag; _ = q2; _ = q3
}
`
	fi := Extract("/proj/repo.go", []byte(src), cat)
	if fi.TableVars["userTbl"] != "users" {
		t.Fatalf("expected userTbl -> users, got %+v", fi.TableVars)
	}
	// VarRefAt on interpolated userTbl
	vrOff := strings.Index(src, "+ userTbl +") + 3
	vr := fi.VarRefAt(vrOff)
	if vr == nil || vr.Table != "users" {
		t.Errorf("expected VarRefAt -> users, got %+v", vr)
	}
	// Fragment `WHERE u.nope = $1` inherits `u -> users` from q1 in same function!
	ds := Analyze(fi, cat)
	foundInheritedErr := false
	for _, d := range ds {
		if strings.Contains(d.Message, "Column 'nope' does not exist on table 'users'") {
			foundInheritedErr = true
			if src[d.Start:d.End] != "nope" {
				t.Errorf("diagnostic Go range mismatch: %q", src[d.Start:d.End])
			}
		}
	}
	if !foundInheritedErr {
		t.Errorf("expected fragment to inherit alias u->users and flag nope, got %+v", ds)
	}
}

func TestRawSQL_RefsCompletionJoinConditionsAndSemanticTokens(t *testing.T) {
	_, cat := buildRichCat(t)
	src := `package repo
func f() {
	userTbl := (models.User{}).TableName()
	q := ` + "`" + `SELECT u.id, u.email AS mail, COUNT(*) total_cnt
FROM ` + "` + userTbl + `" + ` u
LEFT JOIN orders o ON o.user_id = u.id
WHERE u.email = 'a@b' AND o.total > 10 AND u.id = $1
GROUP BY mail
ORDER BY total_cnt` + "`" + `
	_ = q
}
`
	fi := Extract("/proj/repo.go", []byte(src), cat)
	if len(fi.Docs) == 0 {
		t.Fatal("expected extracted doc")
	}
	d := fi.Docs[0]
	sc := fi.BuildScope(d, cat)

	// RefAt on qualified col, table alias, col alias definition & usage
	vEmail := strings.Index(d.Text, "u.email") + 3
	if ref := d.RefAt(vEmail, sc); ref == nil || ref.Column == nil || ref.Column.Name != "email" {
		t.Errorf("expected email column ref, got %+v", ref)
	}
	vMailDef := strings.Index(d.Text, "AS mail") + 4
	if ref := d.RefAt(vMailDef, sc); ref == nil || ref.ColAlias == nil || ref.ColAlias.Name != "mail" || ref.Column == nil || ref.Column.Name != "email" {
		t.Errorf("expected column alias def ref resolved to email, got %+v", ref)
	}
	vMailUse := strings.Index(d.Text, "GROUP BY mail") + 10
	if ref := d.RefAt(vMailUse, sc); ref == nil || ref.ColAlias == nil || ref.ColAlias.Name != "mail" {
		t.Errorf("expected column alias usage ref in GROUP BY, got %+v", ref)
	}
	vAlias := strings.Index(d.Text, "FROM users u") + 11
	if ref := d.RefAt(vAlias, sc); ref == nil || ref.Table == nil || ref.TableAlias != "u" {
		t.Errorf("expected table alias ref 'u', got %+v", ref)
	}
	if len(d.Refs(sc)) < 5 {
		t.Errorf("expected multiple Refs, got %d", len(d.Refs(sc)))
	}
	if len(d.SelectAliases()) != 2 {
		t.Errorf("expected 2 SelectAliases (mail, total_cnt), got %+v", d.SelectAliases())
	}
	if vs, ve, ok := d.TableAliasDef("o"); !ok || d.Text[vs:ve] != "o" {
		t.Errorf("TableAliasDef('o') failed: %d..%d ok=%v", vs, ve, ok)
	}

	// JoinConditions
	conds := JoinConditions(cat.Table("orders"), "o", sc)
	if len(conds) == 0 || !strings.Contains(strings.Join(conds, ","), "o.user_id = u.id") {
		t.Errorf("expected FK join condition o.user_id = u.id, got %v", conds)
	}

	// CompletionAt modes
	docComp := &Doc{Text: "SELECT u.em FROM users u JOIN orders o ON o"}
	if cc := docComp.CompletionAt(strings.Index(docComp.Text, "u.em") + 4); cc.Qualifier != "u" || cc.Prefix != "em" {
		t.Errorf("unexpected qualifier completion: %+v", cc)
	}
	if cc := docComp.CompletionAt(len(docComp.Text)); !cc.WantJoinOn || cc.JoinTable != "orders" || cc.JoinAlias != "o" {
		t.Errorf("unexpected JOIN ON completion: %+v", cc)
	}
	docTbl := &Doc{Text: "SELECT * FROM us"}
	if cc := docTbl.CompletionAt(len(docTbl.Text)); !cc.WantTable || cc.Prefix != "us" {
		t.Errorf("unexpected table completion: %+v", cc)
	}
	// Out-of-bounds clamp must not panic
	_ = docTbl.CompletionAt(-5)
	_ = docTbl.CompletionAt(999)

	// SemanticTokens
	toks := SemanticTokens(fi, cat)
	seenTypes := map[int]bool{}
	for _, tk := range toks {
		seenTypes[tk.Type] = true
	}
	for _, wantType := range []int{SemKeyword, SemFunction, SemType, SemProperty, SemParameter, SemString, SemNumber, SemOperator} {
		if !seenTypes[wantType] {
			t.Errorf("expected semantic token type %d (%s) to be emitted", wantType, SemanticTokenTypes[wantType])
		}
	}
}

func TestRawSQL_QueryCallsScanStructAndCodeActions(t *testing.T) {
	root, cat := buildRichCat(t)

	src := `package repo
import "github.com/goravel/framework/facades"
func LoadUserOrders() {
	var rows []models.UserOrderRow
	q := ` + "`" + `SELECT u.email, u.bio, o.total
FROM users u
LEFT JOIN orders o ON o.user_id = u.id
WHERE u.email = ?` + "`" + `
	_ = facades.Orm().Query().Raw(q, "a@b.com").Scan(&rows)
	_ = facades.DB().Select(&rows, ` + "`SELECT email FROM users WHERE id = ?`" + `, 1, 2)
	var dynArgs []any
	_ = facades.DB().Exec(` + "`DELETE FROM users WHERE id = ?`" + `, dynArgs...)
}
`
	fi := Extract(filepath.Join(root, "app/repo.go"), []byte(src), cat)
	if len(fi.Queries) != 3 {
		t.Fatalf("expected 3 QueryCalls, got %d", len(fi.Queries))
	}

	ds := Analyze(fi, cat)
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, " | ")
	if !strings.Contains(joined, "Column 'total' has no matching field in UserOrderRow") {
		t.Errorf("expected missing scan field warning for total, got %v", msgs)
	}
	if !strings.Contains(joined, "Column 'bio' may be NULL but UserOrderRow.Bio is string") {
		t.Errorf("expected nullable scan info diagnostic for bio, got %v", msgs)
	}
	if !strings.Contains(joined, "1 placeholder(s) but 2 argument(s)") {
		t.Errorf("expected placeholder mismatch for ? query, got %v", msgs)
	}
	if strings.Contains(joined, "DELETE") {
		t.Errorf("variadic/slice args should be opaque and skipped: %v", msgs)
	}

	// GenerateRowStruct on Raw().Scan call
	rawOff := strings.Index(src, ".Raw(q")
	insAt, code, name, ok := GenerateRowStruct(fi, cat, rawOff)
	if !ok || name != "LoadUserOrdersRow" || insAt <= 0 {
		t.Fatalf("GenerateRowStruct failed: ok=%v name=%q insAt=%d", ok, name, insAt)
	}
	if !strings.Contains(code, `database/sql`) || !strings.Contains(code, "Bio sql.NullString") || !strings.Contains(code, "Total sql.NullFloat64") {
		t.Errorf("unexpected generated struct:\n%s", code)
	}

	// AddMissingScanFields targets app/models/user.go and adds Total
	targetFile, rbrace, newText, title, ok := AddMissingScanFields(fi, cat, rawOff)
	if !ok || !strings.HasSuffix(targetFile, "user.go") || rbrace <= 0 || !strings.Contains(newText, "Total *float64") || !strings.Contains(title, "UserOrderRow") {
		t.Errorf("AddMissingScanFields unexpected: ok=%v file=%q rbrace=%d title=%q text=%q", ok, targetFile, rbrace, title, newText)
	}

	// ExpandStar rejects COUNT(*) and supports alias.*
	starSrcText := `package repo
func f() {
	q1 := ` + "`SELECT COUNT(*) FROM users`" + `
	q2 := ` + "`SELECT u.* FROM users u`" + `
	_ = q1; _ = q2
}`
	fiStar := Extract("/proj/s.go", []byte(starSrcText), cat)
	if _, _, _, ok := ExpandStar(fiStar, cat, strings.Index(starSrcText, "COUNT(*)")+6); ok {
		t.Error("ExpandStar must not expand COUNT(*)")
	}
	_, _, repl, ok := ExpandStar(fiStar, cat, strings.Index(starSrcText, "u.*")+2)
	if !ok || !strings.Contains(repl, "u.id") || !strings.Contains(repl, "u.email") {
		t.Errorf("ExpandStar(u.*) unexpected: ok=%v repl=%q", ok, repl)
	}

	// wrapNull & sqlToGo coverage
	if wrapNull("uuid.UUID", true) != "uuid.NullUUID" || wrapNull("time.Time", true) != "sql.NullTime" || wrapNull("int64", true) != "sql.NullInt64" || wrapNull("bool", true) != "sql.NullBool" || wrapNull("any", true) != "any" || wrapNull("Custom", true) != "*Custom" {
		t.Error("unexpected wrapNull mapping")
	}
	if sqlToGo("UUID") != "uuid.UUID" || sqlToGo("TIMESTAMPTZ") != "time.Time" || sqlToGo("BOOLEAN") != "bool" || sqlToGo("BIGINT") != "int64" || sqlToGo("INTEGER") != "int" || sqlToGo("NUMERIC") != "float64" || sqlToGo("VARCHAR(50)") != "string" || sqlToGo("BYTEA") != "any" {
		t.Error("unexpected sqlToGo mapping")
	}
}
