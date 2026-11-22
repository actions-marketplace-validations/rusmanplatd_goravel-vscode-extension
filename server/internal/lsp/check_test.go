package lsp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const migration = "package migrations\n\nimport \"github.com/rusmanplatd/basegoravel/app/facades\"\n\nfunc Up() error {\n\treturn facades.Schema().Create(\"things\", nil)\n}\n"

// project writes files (relative path -> content) into a temp project root.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, c := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func repo(body string) string {
	return "package repositories\n\nimport \"github.com/rusmanplatd/basegoravel/app/facades\"\n\nfunc f() {\n" + body + "\n}\n"
}

const configGo = "package config\n\nimport \"github.com/rusmanplatd/basegoravel/app/facades\"\n\nfunc init() {\n\tc := facades.Config()\n\tc.Add(\"jwt\", map[string]any{\"secret\": c.Env(\"JWT_SECRET\", \"x\")})\n}\n"

// ---------- positive ----------

func TestCheck_Positive_EmptyProjectIsClean(t *testing.T) {
	if code := CheckWithOptions(CheckOptions{Root: project(t, nil)}); code != 0 {
		t.Errorf("empty project should exit 0, got %d", code)
	}
}

func TestCheck_Positive_ValidConfigUsageIsClean(t *testing.T) {
	root := project(t, map[string]string{
		"config/jwt.go":         configGo,
		"app/repositories/r.go": repo(`_ = facades.Config().GetString("jwt.secret")`),
	})
	if code := CheckWithOptions(CheckOptions{Root: root}); code != 0 {
		t.Errorf("valid project should exit 0, got %d", code)
	}
}

func TestCheck_Positive_TestdataDirSkipped(t *testing.T) {
	root := project(t, map[string]string{
		"config/jwt.go":                  configGo,
		"testdata/app/repositories/r.go": repo(`_ = facades.Config().GetString("jwt.nope")`),
	})
	if code := CheckWithOptions(CheckOptions{Root: root}); code != 0 {
		t.Errorf("testdata is skipped, got exit %d", code)
	}
}

func TestCheck_Positive_SkipDirsOption(t *testing.T) {
	root := project(t, map[string]string{
		"config/jwt.go": configGo,
		"legacy/r.go":   repo(`_ = facades.Config().GetString("jwt.nope")`),
	})
	if code := CheckWithOptions(CheckOptions{Root: root, SkipDirs: []string{"legacy"}}); code != 0 {
		t.Errorf("SkipDirs should exclude legacy, got exit %d", code)
	}
}

func TestCheck_Positive_TestFilesIgnored(t *testing.T) {
	root := project(t, map[string]string{
		"config/jwt.go": configGo,
		"app/r_test.go": repo(`_ = facades.Config().GetString("jwt.nope")`),
	})
	if code := CheckWithOptions(CheckOptions{Root: root}); code != 0 {
		t.Errorf("_test.go files are ignored, got exit %d", code)
	}
}

// ---------- negative ----------

func TestCheck_Negative_UnknownConfigKeyFails(t *testing.T) {
	root := project(t, map[string]string{
		"config/jwt.go":         configGo,
		"app/repositories/r.go": repo(`_ = facades.Config().GetString("jwt.nope")`),
	})
	if code := CheckWithOptions(CheckOptions{Root: root}); code != 1 {
		t.Errorf("unknown config key should exit 1, got %d", code)
	}
}

func TestCheck_Negative_ErrorsOnlyIgnoresWarnings(t *testing.T) {
	root := project(t, map[string]string{
		"config/jwt.go":         configGo,
		"app/repositories/r.go": repo(`_ = facades.Config().GetString("jwt.nope")`),
	})
	if code := CheckWithOptions(CheckOptions{Root: root, ErrorsOnly: true}); code != 0 {
		t.Errorf("warnings must not fail with ErrorsOnly, got %d", code)
	}
}

func TestCheck_Negative_OnlySQLIgnoresConfigIssues(t *testing.T) {
	root := project(t, map[string]string{
		"config/jwt.go":         configGo,
		"app/repositories/r.go": repo(`_ = facades.Config().GetString("jwt.nope")`),
	})
	if code := CheckWithOptions(CheckOptions{Root: root, OnlySQL: true}); code != 0 {
		t.Errorf("OnlySQL excludes config diagnostics, got %d", code)
	}
}

func TestCheck_Negative_UnparsableFileDoesNotPanic(t *testing.T) {
	root := project(t, map[string]string{"app/broken.go": "package x\nfunc ("})
	_ = CheckWithOptions(CheckOptions{Root: root})
}

func TestCheck_Negative_NonexistentRootDoesNotPanic(t *testing.T) {
	_ = CheckWithOptions(CheckOptions{Root: "/nonexistent/goravel/root"})
}

// ---------- templates ----------

func TestCheck_Positive_ValidTemplateVarsClean(t *testing.T) {
	root := project(t, map[string]string{
		"resources/views/welcome.tmpl":        "{{ define \"welcome.tmpl\" }}{{ template \"layouts/header.tmpl\" . }}<h1>{{ .title }}</h1>{{ end }}",
		"resources/views/layouts/header.tmpl": "{{ define \"layouts/header.tmpl\" }}<title>{{ .app_name }}</title>{{ end }}",
		"routes/web.go": `package routes

func Web(ctx http.Context) {
	_ = ctx.Response().View().Make("welcome.tmpl", map[string]any{
		"title":    "Hello",
		"app_name": "Goravel",
	})
}
`,
	})
	if code := CheckWithOptions(CheckOptions{Root: root, OnlyTemplates: true}); code != 0 {
		t.Errorf("valid template variables should exit 0, got %d", code)
	}
}

func TestCheck_Positive_ModuleRoutePassesTemplateVars(t *testing.T) {
	root := project(t, map[string]string{
		"resources/views/auth/signin.tmpl": "{{ define \"auth/signin.tmpl\" }}{{ .supabase_url }}{{ end }}",
		"modules/pusat/routes/web.go": `package routes

func Web(ctx http.Context) {
	_ = ctx.Response().View().Make("auth/signin.tmpl", map[string]any{
		"supabase_url": "https://example.supabase.co",
	})
}
`,
	})
	if code := CheckWithOptions(CheckOptions{Root: root, OnlyTemplates: true}); code != 0 {
		t.Errorf("module route view variables should be indexed and exit 0, got %d", code)
	}
}

func TestCheck_Positive_NonTemplatePublicHTMLIgnored(t *testing.T) {
	root := project(t, map[string]string{
		"public/index.html": "<div>{{ .not_a_go_template }}</div>",
	})
	if code := CheckWithOptions(CheckOptions{Root: root, OnlyTemplates: true}); code != 0 {
		t.Errorf("public/*.html outside views should not be checked as templates, got %d", code)
	}
}

func TestCheck_Negative_UnknownTemplateVariableFails(t *testing.T) {
	root := project(t, map[string]string{
		"resources/views/welcome.tmpl": "{{ define \"welcome.tmpl\" }}<h1>{{ .missing_var }}</h1>{{ end }}",
		"routes/web.go": `package routes

func Web(ctx http.Context) {
	_ = ctx.Response().View().Make("welcome.tmpl", map[string]any{
		"title": "Hello",
	})
}
`,
	})
	if code := CheckWithOptions(CheckOptions{Root: root, OnlyTemplates: true}); code != 1 {
		t.Errorf("unknown template variable should exit 1 with OnlyTemplates, got %d", code)
	}
	if code := CheckWithOptions(CheckOptions{Root: root}); code != 1 {
		t.Errorf("unknown template variable should exit 1 in full check, got %d", code)
	}
	if code := CheckWithOptions(CheckOptions{Root: root, OnlySQL: true}); code != 0 {
		t.Errorf("OnlySQL should ignore template diagnostics, got %d", code)
	}
	if code := CheckWithOptions(CheckOptions{Root: root, OnlySQL: true, OnlyTemplates: true}); code != 1 {
		t.Errorf("OnlySQL + OnlyTemplates should catch template variable errors, got %d", code)
	}
}

func TestCheck_Negative_OnlyTemplatesIgnoresConfigIssues(t *testing.T) {
	root := project(t, map[string]string{
		"config/jwt.go":                configGo,
		"app/repositories/r.go":        repo(`_ = facades.Config().GetString("jwt.nope")`),
		"resources/views/welcome.tmpl": "{{ define \"welcome.tmpl\" }}<p>ok</p>{{ end }}",
	})
	if code := CheckWithOptions(CheckOptions{Root: root, OnlyTemplates: true}); code != 0 {
		t.Errorf("OnlyTemplates should ignore config diagnostics, got %d", code)
	}
}

// ---------- helper functions ----------

func TestRuleNameAt(t *testing.T) {
	text := "required|max_len:255|uuid"
	cases := []struct {
		off  int
		want string
	}{{2, "required"}, {12, "max_len"}, {22, "uuid"}}
	for _, c := range cases {
		if got, _, _ := ruleNameAt(text, c.off); got != c.want {
			t.Errorf("ruleNameAt(%d)=%q want %q", c.off, got, c.want)
		}
	}
	for _, off := range []int{-1, len(text) + 1} {
		if got, _, _ := ruleNameAt(text, off); got != "" {
			t.Errorf("out-of-range offset %d should return empty, got %q", off, got)
		}
	}
}

func TestIdentWordAt(t *testing.T) {
	if got := identWordAt("foo_bar baz", 2); got != "foo_bar" {
		t.Errorf("got %q", got)
	}
	if got := identWordAt("foo bar", 3); got != "foo" {
		t.Errorf("edge of word should still resolve, got %q", got)
	}
	if got := identWordAt("a  b", 2); got != "" {
		t.Errorf("between spaces should be empty, got %q", got)
	}
	if identWordAt("abc", -1) != "" || identWordAt("abc", 99) != "" {
		t.Error("out of range offsets must return empty")
	}
}

func TestFindWordOccurrences(t *testing.T) {
	got := findWordOccurrences("Name name username name_x NAME", "name")
	want := [][2]int{{0, 4}, {5, 9}, {26, 30}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if got := findWordOccurrences("abc", "zzz"); len(got) != 0 {
		t.Errorf("no match expected, got %v", got)
	}
	if got := findWordOccurrences("", "x"); len(got) != 0 {
		t.Errorf("empty source: %v", got)
	}
}
