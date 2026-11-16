package rawsql

import "testing"

func TestParseSelectListWithCTE(t *testing.T) {
	sql := `
	WITH active_users AS (
		SELECT id, email FROM users WHERE deleted_at IS NULL
	)
	SELECT id, email, role_name AS role
	FROM active_users`

	cols, star := ParseSelectList(sql)
	if star {
		t.Fatalf("expected star=false")
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 output columns, got %d: %+v", len(cols), cols)
	}
	if cols[0].Name != "id" || cols[1].Name != "email" || cols[2].Name != "role" {
		t.Fatalf("unexpected columns: %+v", cols)
	}
}

func TestCheckSQLSyntaxDollarAndEscapeStrings(t *testing.T) {
	issues := checkSQLSyntax(`SELECT $tag$hello 'world'$tag$, E'it\'s ok' FROM users`, false)
	if len(issues) != 0 {
		t.Fatalf("expected no syntax issues, got %+v", issues)
	}

	unclosed := checkSQLSyntax(`SELECT $$unclosed FROM users`, false)
	if len(unclosed) == 0 {
		t.Fatalf("expected syntax issue for unclosed dollar-quoted string")
	}
}

