package framework

import (
	"testing"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

func FuzzTemplateDiagnostics(f *testing.F) {
	cat := indexer.NewCatalog()
	cat.Views["a.tmpl"] = indexer.Location{File: "/p/resources/views/a.tmpl"}
	seeds := []string{
		"",
		"{{",
		"}}",
		"{{ define \"a.tmpl\" }}{{ if .x }}{{ range .y }}{{ end }}{{ end }}{{ end }}",
		"{{ template \"missing\" . }} {{ .foo | bar }}",
		"{{- /* comment */ -}} {{ with .user }}{{ .Name }}{{ end }}",
		"<a href=\"/x\">{{ $a, $b := .items }}",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		for _, d := range TemplateDiagnostics("/p/resources/views/a.tmpl", src, cat) {
			if d.Start < 0 || d.End < d.Start || d.End > len(src) {
				t.Fatalf("out-of-bounds diag [%d,%d] for len=%d src=%q", d.Start, d.End, len(src), src)
			}
		}
		_ = TemplateDocumentSymbols("/p/resources/views/a.tmpl", src)
		_ = TemplateHover("/p/resources/views/a.tmpl", src, cat, len(src)/2)
		_ = TemplateCompletions("/p/resources/views/a.tmpl", src, cat, len(src)/2)
	})
}
