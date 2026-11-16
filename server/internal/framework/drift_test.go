package framework

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

// driftProject clones the fixture, lets mutate edit the City model source, and returns the model path, new source and catalog.
func driftProject(t *testing.T, mutate func(string) string) (string, string, *indexer.Catalog) {
	t.Helper()
	root := t.TempDir()
	err := filepath.Walk(fixture, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fixture, p)
		dst := filepath.Join(root, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(root, "app/models/ref_geo_city.go")
	b, _ := os.ReadFile(modelPath)
	src := mutate(string(b))
	if err := os.WriteFile(modelPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return modelPath, src, indexer.Build(root)
}

func identity(s string) string { return s }

// ---------- positive ----------

func TestDrift_Positive_ModelMatchesMigration(t *testing.T) {
	path, src, cat := driftProject(t, identity)
	for _, d := range DriftDiagnostics(path, src, cat) {
		if d.Severity <= 2 {
			t.Errorf("in-sync model must not warn: %+v", d)
		}
	}
}

func TestDrift_Positive_NonModelFileIgnored(t *testing.T) {
	_, _, cat := driftProject(t, identity)
	src := "package services\n\ntype RefGeoCity struct{ Extra string }\n"
	if ds := DriftDiagnostics("/p/app/services/x.go", src, cat); len(ds) != 0 {
		t.Errorf("only /app/models/ files are drift-checked, got %+v", ds)
	}
}

// ---------- negative ----------

func TestDrift_Negative_MigrationColumnMissingInModel(t *testing.T) {
	path, src, cat := driftProject(t, func(s string) string {
		i := strings.Index(s, "\t// @Description City code")
		j := strings.Index(s, "Code *string")
		k := strings.Index(s[j:], "\n") + j
		return s[:i] + s[k+1:]
	})
	ds := DriftDiagnostics(path, src, cat)
	var found *Diag
	for i, d := range ds {
		if strings.Contains(d.Message, "missing migration column(s)") {
			found = &ds[i]
		}
	}
	if found == nil {
		t.Fatalf("expected missing-column drift warning, got %+v", ds)
	}
	if found.Severity != 2 || !strings.Contains(found.Message, "code") {
		t.Errorf("unexpected drift diagnostic %+v", found)
	}
	if src[found.Start:found.End] != "RefGeoCity" {
		t.Errorf("range should cover the struct name, got %q", src[found.Start:found.End])
	}
}

func TestDrift_Negative_ModelFieldNotInMigration(t *testing.T) {
	path, src, cat := driftProject(t, func(s string) string {
		return strings.Replace(s, "\tName string", "\tNickname string `gorm:\"type:varchar(50)\" json:\"nickname\"`\n\tName string", 1)
	})
	ds := DriftDiagnostics(path, src, cat)
	ok := false
	for _, d := range ds {
		if strings.Contains(d.Message, "Model field 'Nickname'") && strings.Contains(d.Message, "not found in migration") {
			ok = true
			if d.Severity != 3 {
				t.Errorf("extra model field should be info, got %d", d.Severity)
			}
		}
	}
	if !ok {
		t.Errorf("expected extra-field drift info, got %+v", ds)
	}
}

func TestDrift_Negative_SyntaxErrorDoesNotPanic(t *testing.T) {
	path, _, cat := driftProject(t, identity)
	_ = DriftDiagnostics(path, "package models\nfunc (", cat)
	_ = DriftDiagnostics(path, "", cat)
}

func TestDrift_Negative_EmptyCatalog(t *testing.T) {
	if ds := DriftDiagnostics("/p/app/models/a.go", "package models\ntype A struct{}\n", indexer.NewCatalog()); len(ds) != 0 {
		t.Errorf("empty catalog has nothing to compare, got %+v", ds)
	}
}
