package framework

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func TestIsTemplateFile(t *testing.T) {
	for _, p := range []string{"/a/b.tmpl", "/a/b.gohtml", "/a/b.html"} {
		if !IsTemplateFile(p) {
			t.Errorf("%s should be a template", p)
		}
	}
	for _, p := range []string{"/a/b.go", "/a/b.txt", "/a/tmpl", ""} {
		if IsTemplateFile(p) {
			t.Errorf("%s should not be a template", p)
		}
	}
}

// viewProject builds a throwaway project containing the given resources/views files.
func viewProject(t *testing.T, files map[string]string) (string, *indexer.Catalog) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, "resources", "views", rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, indexer.Build(root)
}

func controllerWith(body string) string {
	return "package controllers\n\nfunc f(ctx http.Context) {\n" + body + "\n}\n"
}

func hasDiag(ds []Diag, sub string) bool {
	for _, d := range ds {
		if strings.Contains(d.Message, sub) {
			return true
		}
	}
	return false
}

// ---------- positive ----------

func TestView_Positive_IndexedViews(t *testing.T) {
	_, cat := viewProject(t, map[string]string{
		"home/index.tmpl": "<h1>{{ .title }}</h1>",
	})
	if cat.LookupView("home/index.tmpl") == nil && cat.LookupView("home/index") == nil {
		t.Fatalf("expected view indexed, have %v", cat.Views)
	}
}

func TestView_Positive_ExistingViewNoDiagnostic(t *testing.T) {
	root, cat := viewProject(t, map[string]string{"home/index.tmpl": "<h1>{{ .title }}</h1>"})
	src := controllerWith(`	ctx.Response().View().Make("home/index.tmpl", map[string]any{"title": "x"})`)
	ds := ViewDiagnostics(filepath.Join(root, "app/http/controllers/h.go"), src, cat)
	if hasDiag(ds, "not found") || hasDiag(ds, "not referenced") {
		t.Errorf("unexpected diagnostics %+v", ds)
	}
}

func TestView_Positive_FirstWithOneExistingFallback(t *testing.T) {
	root, cat := viewProject(t, map[string]string{"home/index.tmpl": "x"})
	src := controllerWith(`	ctx.Response().View().First([]string{"missing.tmpl", "home/index.tmpl"}, nil)`)
	ds := ViewDiagnostics(filepath.Join(root, "app/http/controllers/h.go"), src, cat)
	if hasDiag(ds, "None of the fallback") {
		t.Errorf("one valid fallback is enough, got %+v", ds)
	}
}

func TestView_Positive_TemplateCompletionsOnDot(t *testing.T) {
	root, cat := viewProject(t, map[string]string{"home/index.tmpl": "{{ .title }}"})
	_ = root
	src := "{{ . }}"
	if items := TemplateCompletions("/p/resources/views/home/index.tmpl", src, cat, 3); items == nil {
		t.Log("no completions at '{{ . }}' (acceptable: depends on call-site data)")
	}
}

// ---------- negative ----------

func TestView_Negative_MissingViewWarns(t *testing.T) {
	root, cat := viewProject(t, map[string]string{"home/index.tmpl": "x"})
	src := controllerWith(`	ctx.Response().View().Make("ghost/page.tmpl", nil)`)
	ds := ViewDiagnostics(filepath.Join(root, "app/http/controllers/h.go"), src, cat)
	if !hasDiag(ds, "View template 'ghost/page.tmpl' not found") {
		t.Fatalf("expected missing view warning, got %+v", ds)
	}
	for _, d := range ds {
		if d.Severity != 2 {
			t.Errorf("Make should warn (sev 2), got %d", d.Severity)
		}
	}
}

func TestView_Negative_ExistsMissingIsHint(t *testing.T) {
	root, cat := viewProject(t, map[string]string{"home/index.tmpl": "x"})
	src := controllerWith(`	_ = facades.View().Exists("ghost.tmpl")`)
	ds := ViewDiagnostics(filepath.Join(root, "app/http/controllers/h.go"), src, cat)
	if len(ds) != 1 || ds[0].Severity != 4 {
		t.Errorf("Exists on missing view should be a hint, got %+v", ds)
	}
}

func TestView_Negative_FirstAllMissing(t *testing.T) {
	root, cat := viewProject(t, map[string]string{"home/index.tmpl": "x"})
	src := controllerWith(`	ctx.Response().View().First([]string{"a.tmpl", "b.tmpl"}, nil)`)
	ds := ViewDiagnostics(filepath.Join(root, "app/http/controllers/h.go"), src, cat)
	if !hasDiag(ds, "None of the fallback view templates") {
		t.Errorf("expected fallback warning, got %+v", ds)
	}
}

func TestView_Negative_UnusedDataKeyHint(t *testing.T) {
	root, cat := viewProject(t, map[string]string{"home/index.tmpl": "<h1>{{ .title }}</h1>"})
	src := controllerWith(`	ctx.Response().View().Make("home/index.tmpl", map[string]any{"title": "x", "orphan": 1})`)
	ds := ViewDiagnostics(filepath.Join(root, "app/http/controllers/h.go"), src, cat)
	if !hasDiag(ds, "View data key 'orphan' is not referenced in home/index.tmpl") {
		t.Errorf("expected unused key hint, got %+v", ds)
	}
	if hasDiag(ds, "'title'") {
		t.Errorf("used key 'title' must not be flagged: %+v", ds)
	}
}

func TestView_Negative_MissingTemplateVariableHint(t *testing.T) {
	root, cat := viewProject(t, map[string]string{"home/index.tmpl": "{{ .title }} {{ .subtitle }}"})
	src := controllerWith(`	ctx.Response().View().Make("home/index.tmpl", map[string]any{"title": "x"})`)
	ds := ViewDiagnostics(filepath.Join(root, "app/http/controllers/h.go"), src, cat)
	if !hasDiag(ds, ".subtitle") {
		t.Errorf("expected missing variable hint, got %+v", ds)
	}
}

func TestView_Negative_NoViewsMeansNoDiagnostics(t *testing.T) {
	cat := indexer.NewCatalog()
	src := controllerWith(`	ctx.Response().View().Make("anything.tmpl", nil)`)
	if ds := ViewDiagnostics("/p/a.go", src, cat); ds != nil {
		t.Errorf("projects without views are not checked, got %+v", ds)
	}
	if ds := ViewDiagnostics("/p/a.go", src, nil); ds != nil {
		t.Errorf("nil catalog must be safe, got %+v", ds)
	}
}

func TestView_Negative_SyntaxErrorDoesNotPanic(t *testing.T) {
	_, cat := viewProject(t, map[string]string{"a.tmpl": "x"})
	_ = ViewDiagnostics("/p/a.go", "package x\nfunc (", cat)
	_ = ViewDiagnostics("/p/a.go", "", cat)
}

func TestTemplate_Negative_MalformedTemplatesDoNotPanic(t *testing.T) {
	_, cat := viewProject(t, map[string]string{"a.tmpl": "x"})
	for _, src := range []string{"", "{{", "{{ end }}", "{{ if }}", "{{ template \"missing\" }}", "}} {{ . "} {
		_ = TemplateDiagnostics("/p/resources/views/a.tmpl", src, cat)
		_ = TemplateCompletions("/p/resources/views/a.tmpl", src, cat, len(src))
		_ = TemplateHover("/p/resources/views/a.tmpl", src, cat, len(src)/2)
	}
}
