package lsp

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/rusmanplatd/goravel-lsp/internal/framework"
	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
	"github.com/rusmanplatd/goravel-lsp/internal/orm"
	"github.com/rusmanplatd/goravel-lsp/internal/rawsql"
)

// CheckOptions configures CLI project checking (like golangci-lint run).
type CheckOptions struct {
	Root          string
	OnlySQL       bool
	OnlyTemplates bool
	ErrorsOnly    bool
	OutFormat     string // "text" | "github-actions"
	SkipDirs      []string
	SQLSource     indexer.SQLSource
}

// Check prints SQL and Goravel ORM/Config/Validation/Template diagnostics for every Go and template file in a project (CLI: --check <root>).
// It returns a non-zero exit code when issues are found.
func Check(root string) int {
	return CheckWithOptions(CheckOptions{Root: root})
}

// CheckSQL prints only SQL and ORM query diagnostics for every Go file in a project (CLI: --check-sql <root>).
// It returns a non-zero exit code when issues are found.
func CheckSQL(root string) int {
	return CheckWithOptions(CheckOptions{Root: root, OnlySQL: true})
}

// CheckTemplates prints only Go HTML template (.tmpl/.gohtml) and View() diagnostics for a project (CLI: --check-templates <root>).
// It returns a non-zero exit code when issues are found.
func CheckTemplates(root string) int {
	return CheckWithOptions(CheckOptions{Root: root, OnlyTemplates: true})
}

// CheckWithOptions runs static analysis with the given CheckOptions and returns an exit code (0 = clean, 1 = issues found).
func CheckWithOptions(opts CheckOptions) int {
	root := opts.Root
	if root == "" {
		root = "."
	}
	maxSeverity := 2
	if opts.ErrorsOnly {
		maxSeverity = 1
	}
	gha := opts.OutFormat == "github-actions" || (opts.OutFormat == "" && os.Getenv("GITHUB_ACTIONS") == "true")

	skipDirMap := map[string]bool{
		"vendor":       true,
		"node_modules": true,
		".git":         true,
		"examples":     true,
		"testdata":     true,
	}
	for _, sd := range opts.SkipDirs {
		if s := strings.TrimSpace(sd); s != "" {
			skipDirMap[s] = true
		}
	}

	cat := indexer.BuildWithOptions(root, indexer.BuildOptions{
		LiveDB:    indexer.LiveDBEnabled,
		SQLSource: opts.SQLSource,
	})
	sqlCat := cat.ForSQL(opts.SQLSource)
	type checkIssue struct {
		file string
		line int
		col  int
		sev  int
		msg  string
	}
	type fileResult struct {
		docs   int
		issues []checkIssue
	}

	runAll := !opts.OnlySQL && !opts.OnlyTemplates
	runSQL := runAll || opts.OnlySQL
	runTemplates := runAll || opts.OnlyTemplates

	var files []string
	var tmplFiles []string
	seenTmpl := map[string]bool{}
	addTmpl := func(p string) {
		clean := filepath.Clean(p)
		if !seenTmpl[clean] {
			seenTmpl[clean] = true
			tmplFiles = append(tmplFiles, clean)
		}
	}

	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipDirMap[d.Name()] {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			files = append(files, p)
			return nil
		}
		if runTemplates {
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".tmpl" || ext == ".gohtml" {
				addTmpl(p)
			}
		}
		return nil
	})
	if runTemplates {
		for _, vi := range cat.ViewItems {
			if vi != nil && vi.Loc.File != "" {
				if rel, err := filepath.Rel(root, vi.Loc.File); err == nil {
					skipped := false
					for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
						if skipDirMap[part] {
							skipped = true
							break
						}
					}
					if skipped {
						continue
					}
				}
				addTmpl(vi.Loc.File)
			}
		}
	}
	sort.Strings(files)
	sort.Strings(tmplFiles)

	results := make([]fileResult, len(files))
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(files) && len(files) > 0 {
		workers = len(files)
	}
	jobs := make(chan int, len(files))
	for i := range files {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				p := files[idx]
				b, err := os.ReadFile(p)
				if err != nil {
					continue
				}
				src := string(b)
				var res fileResult
				if runSQL {
					fi := rawsql.Extract(p, b, sqlCat)
					res.docs = len(fi.Docs)
					for _, dg := range rawsql.Analyze(fi, sqlCat) {
						if dg.Severity > maxSeverity {
							continue
						}
						pos := offsetToPos(src, dg.Start)
						res.issues = append(res.issues, checkIssue{p, pos.Line + 1, pos.Character + 1, dg.Severity, dg.Message})
					}
					fa := orm.AnalyzeFile(p, b, sqlCat)
					var ormDiags []rawsql.Diag
					if !runAll {
						ormDiags = fa.SQLDiagnostics(sqlCat)
					} else {
						ormDiags = fa.Diagnostics(sqlCat)
					}
					for _, dg := range ormDiags {
						if dg.Severity > maxSeverity {
							continue
						}
						pos := offsetToPos(src, dg.Start)
						res.issues = append(res.issues, checkIssue{p, pos.Line + 1, pos.Character + 1, dg.Severity, dg.Message})
					}
				}
				if runAll || runTemplates {
					var fDiags []framework.Diag
					if runAll {
						fDiags = append(fDiags, framework.RequestDiagnostics(p, src, cat)...)
						fDiags = append(fDiags, framework.ControllerDiagnostics(p, src, cat)...)
						fDiags = append(fDiags, framework.DriftDiagnostics(p, src, cat)...)
					}
					fDiags = append(fDiags, framework.ViewDiagnostics(p, src, cat)...)
					for _, dg := range fDiags {
						if dg.Severity > maxSeverity {
							continue
						}
						pos := offsetToPos(src, dg.Start)
						res.issues = append(res.issues, checkIssue{p, pos.Line + 1, pos.Character + 1, dg.Severity, dg.Message})
					}
				}
				results[idx] = res
			}
		}()
	}
	wg.Wait()

	tmplResults := make([]fileResult, len(tmplFiles))
	if runTemplates && len(tmplFiles) > 0 {
		tWorkers := runtime.NumCPU()
		if tWorkers < 1 {
			tWorkers = 1
		}
		if tWorkers > len(tmplFiles) {
			tWorkers = len(tmplFiles)
		}
		tJobs := make(chan int, len(tmplFiles))
		for i := range tmplFiles {
			tJobs <- i
		}
		close(tJobs)
		var twg sync.WaitGroup
		for w := 0; w < tWorkers; w++ {
			twg.Add(1)
			go func() {
				defer twg.Done()
				for idx := range tJobs {
					p := tmplFiles[idx]
					b, err := os.ReadFile(p)
					if err != nil {
						continue
					}
					src := string(b)
					var res fileResult
					for _, dg := range framework.TemplateDiagnostics(p, src, cat) {
						if dg.Severity > maxSeverity {
							continue
						}
						pos := offsetToPos(src, dg.Start)
						res.issues = append(res.issues, checkIssue{p, pos.Line + 1, pos.Character + 1, dg.Severity, dg.Message})
					}
					tmplResults[idx] = res
				}
			}()
		}
		twg.Wait()
	}

	docs, issues := 0, 0
	emit := func(it checkIssue) {
		rel := it.file
		if r, err := filepath.Rel(root, it.file); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		}
		if gha {
			level := "error"
			if it.sev > 1 {
				level = "warning"
			}
			fmt.Printf("::%s file=%s,line=%d,col=%d::%s\n", level, rel, it.line, it.col, it.msg)
		} else {
			fmt.Printf("%s:%d:%d: %s\n", it.file, it.line, it.col, it.msg)
		}
		issues++
	}
	for _, res := range results {
		docs += res.docs
		for _, it := range res.issues {
			emit(it)
		}
	}
	for _, res := range tmplResults {
		for _, it := range res.issues {
			emit(it)
		}
	}
	fmt.Printf("%d sql docs, %d tables, %d routes, %d configs, %d rules, %d templates, %d issues\n",
		docs, len(sqlCat.Tables), len(cat.Routes), len(cat.Configs), len(cat.Rules), len(tmplFiles), issues)
	if issues > 0 {
		return 1
	}
	return 0
}
