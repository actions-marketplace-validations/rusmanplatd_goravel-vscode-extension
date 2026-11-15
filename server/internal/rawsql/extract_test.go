package rawsql

import (
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func TestExtract_NonSQLStringsIgnored(t *testing.T) {
	src := []byte(`package seeders

func seedChecklistItems() {
	items := []struct {
		Name     string
		Resolved bool
	}{
		{"Create database schema", true},
		{"Add integration tests", false},
		{"Run database migrations", false},
		{"Update environment variables", false},
		{"Update API documentation", false},
		{"Select a project", false},
		{"Delete this item", false},
		{"From the dashboard", false},
	}
	_ = items
}
`)
	fi := Extract("project_seeder.go", src, indexer.NewCatalog())
	if len(fi.Docs) != 0 {
		for _, d := range fi.Docs {
			t.Errorf("unexpected SQL doc extracted: %q", d.Text)
		}
	}
}

func TestExtract_ActualSQLDetected(t *testing.T) {
	src := []byte(`package repo

func queries() {
	_ = "SELECT id, name FROM users WHERE active = true"
	_ = "UPDATE users SET name = $1 WHERE id = $2"
	_ = "INSERT INTO users (name) VALUES ($1)"
	_ = "DELETE FROM users WHERE id = $1"
}
`)
	fi := Extract("user_repo.go", src, indexer.NewCatalog())
	if len(fi.Docs) != 4 {
		t.Fatalf("expected 4 SQL docs, got %d", len(fi.Docs))
	}
}

