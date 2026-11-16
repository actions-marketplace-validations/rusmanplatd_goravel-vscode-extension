package framework

import (
	"strings"
	"testing"
)

const tplPath = "/p/resources/views/b.tmpl"

func tplDiags(t *testing.T, src string) []Diag {
	t.Helper()
	_, cat := viewProject(t, map[string]string{
		"layout.tmpl": `{{ define "content" }}x{{ end }}`,
		"a.tmpl":      "x",
	})
	return TemplateDiagnostics(tplPath, src, cat)
}

// ---------- positive ----------

func TestTemplate_Positive_BalancedBlocksClean(t *testing.T) {
	for name, src := range map[string]string{
		"plain text":   "<p>hello</p>",
		"range":        "{{ range .x }}{{ end }}",
		"if else":      "{{ if .x }}{{ else }}{{ end }}",
		"nested":       "{{ if .x }}{{ range .y }}{{ end }}{{ end }}",
		"define block": `{{ define "x" }}a{{ end }}`,
		"known define": `{{ template "content" . }}`,
		"known file":   `{{ template "a.tmpl" . }}`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, d := range tplDiags(t, src) {
				if d.Severity == 1 && (strings.Contains(d.Message, "Unclosed") || strings.Contains(d.Message, "Unexpected")) {
					t.Errorf("unexpected structural error: %+v", d)
				}
			}
		})
	}
}

func TestTemplate_Positive_IsTemplateFileGuard(t *testing.T) {
	if !IsTemplateFile(tplPath) {
		t.Error("tmpl should be recognised")
	}
}

// ---------- negative ----------

func TestTemplate_Negative_UnclosedBlock(t *testing.T) {
	for _, tc := range []struct{ src, block string }{
		{"{{ if .x }}a", "{{ if }}"},
		{"{{ range .x }}a", "{{ range }}"},
		{"{{ with .x }}a", "{{ with }}"},
	} {
		ds := tplDiags(t, tc.src)
		if !hasDiag(ds, "Unclosed '"+tc.block+"' block") {
			t.Errorf("%q: expected unclosed %s error, got %+v", tc.src, tc.block, ds)
		}
		for _, d := range ds {
			if d.Severity != 1 {
				t.Errorf("unclosed block should be an error: %+v", d)
			}
		}
	}
}

func TestTemplate_Negative_StrayEnd(t *testing.T) {
	ds := tplDiags(t, "{{ end }}")
	if !hasDiag(ds, "Unexpected '{{ end }}'") {
		t.Errorf("expected stray end error, got %+v", ds)
	}
}

func TestTemplate_Negative_ExtraEndAfterBalanced(t *testing.T) {
	ds := tplDiags(t, "{{ if .x }}{{ end }}{{ end }}")
	if !hasDiag(ds, "Unexpected '{{ end }}'") {
		t.Errorf("expected stray end error, got %+v", ds)
	}
}

func TestTemplate_Negative_MissingIncludedTemplate(t *testing.T) {
	src := `{{ template "nope" . }}`
	ds := tplDiags(t, src)
	if !hasDiag(ds, "Included template 'nope' not found") {
		t.Fatalf("expected missing include warning, got %+v", ds)
	}
	for _, d := range ds {
		if d.Severity == 2 && src[d.Start:d.End] != "nope" {
			t.Errorf("range should cover the template name, got %q", src[d.Start:d.End])
		}
	}
}

func TestTemplate_Negative_UnknownVariable(t *testing.T) {
	ds := tplDiags(t, "{{ len .x }}")
	if !hasDiag(ds, "Unknown template variable '.x'") {
		t.Errorf("expected unknown variable error, got %+v", ds)
	}
}

func TestTemplate_Negative_DiagnosticRangesWithinSource(t *testing.T) {
	for _, src := range []string{"{{ if .x }}a", "{{ end }}", `{{ template "nope" . }}`, "{{ len .x }}", "{{", "{{ if"} {
		for _, d := range tplDiags(t, src) {
			if d.Start < 0 || d.End < d.Start || d.End > len(src) {
				t.Errorf("%q: range [%d,%d] outside source", src, d.Start, d.End)
			}
		}
	}
}
