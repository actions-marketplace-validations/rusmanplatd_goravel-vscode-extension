package framework

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

type viewNameSpan struct {
	Name  string
	Start int
	End   int
}

type viewCall struct {
	Method      string // Make | First | Exists | PathView | Share | Shared | LoadViewsFrom | GetShared | RegisteredViews
	Names       []viewNameSpan
	ShareKey    *viewNameSpan
	DataMap     *ast.CompositeLit
	MethodStart int
	MethodEnd   int
}

func findViewCalls(path, src string) ([]viewCall, *token.FileSet) {
	if !strings.Contains(src, "View") && !strings.Contains(src, ".Make(") && !strings.Contains(src, ".First(") && !strings.Contains(src, ".Exists(") {
		return nil, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil || f == nil {
		return nil, nil
	}

	localMaps := map[string]*ast.CompositeLit{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range stmt.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && i < len(stmt.Rhs) {
					if cl, ok := stmt.Rhs[i].(*ast.CompositeLit); ok {
						localMaps[ident.Name] = cl
					}
				}
			}
		case *ast.ValueSpec:
			for i, ident := range stmt.Names {
				if i < len(stmt.Values) {
					if cl, ok := stmt.Values[i].(*ast.CompositeLit); ok {
						localMaps[ident.Name] = cl
					}
				}
			}
		}
		return true
	})

	var out []viewCall
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		mStart := fset.Position(sel.Sel.Pos()).Offset
		mEnd := fset.Position(sel.Sel.End()).Offset

		// Check path.View("...")
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "path" && sel.Sel.Name == "View" && len(call.Args) > 0 {
			if sp, ok := extractStringSpan(fset, call.Args[0]); ok {
				out = append(out, viewCall{
					Method:      "PathView",
					Names:       []viewNameSpan{sp},
					MethodStart: mStart,
					MethodEnd:   mEnd,
				})
			}
			return true
		}

		recvStart := fset.Position(sel.X.Pos()).Offset
		recvEnd := fset.Position(sel.X.End()).Offset
		if recvStart < 0 || recvEnd > len(src) || recvStart >= recvEnd {
			return true
		}
		recvText := src[recvStart:recvEnd]
		if !strings.Contains(recvText, "View") && !strings.Contains(strings.ToLower(recvText), "view") {
			return true
		}

		switch sel.Sel.Name {
		case "GetShared", "RegisteredViews":
			out = append(out, viewCall{
				Method:      sel.Sel.Name,
				MethodStart: mStart,
				MethodEnd:   mEnd,
			})
		case "Make":
			if len(call.Args) == 0 {
				return true
			}
			sp, ok := extractStringSpan(fset, call.Args[0])
			if !ok {
				return true
			}
			vc := viewCall{
				Method:      "Make",
				Names:       []viewNameSpan{sp},
				MethodStart: mStart,
				MethodEnd:   mEnd,
			}
			if len(call.Args) > 1 {
				vc.DataMap = resolveDataMapArg(call.Args[1], localMaps)
			}
			out = append(out, vc)
		case "First":
			if len(call.Args) == 0 {
				return true
			}
			sliceLit, ok := call.Args[0].(*ast.CompositeLit)
			if !ok {
				return true
			}
			vc := viewCall{
				Method:      "First",
				MethodStart: mStart,
				MethodEnd:   mEnd,
			}
			for _, elt := range sliceLit.Elts {
				if sp, ok := extractStringSpan(fset, elt); ok {
					vc.Names = append(vc.Names, sp)
				}
			}
			if len(call.Args) > 1 {
				vc.DataMap = resolveDataMapArg(call.Args[1], localMaps)
			}
			if len(vc.Names) > 0 {
				out = append(out, vc)
			}
		case "Exists":
			if len(call.Args) == 0 {
				return true
			}
			if sp, ok := extractStringSpan(fset, call.Args[0]); ok {
				out = append(out, viewCall{
					Method:      "Exists",
					Names:       []viewNameSpan{sp},
					MethodStart: mStart,
					MethodEnd:   mEnd,
				})
			}
		case "Share":
			if len(call.Args) == 0 {
				return true
			}
			if sp, ok := extractStringSpan(fset, call.Args[0]); ok {
				out = append(out, viewCall{
					Method:      "Share",
					ShareKey:    &sp,
					MethodStart: mStart,
					MethodEnd:   mEnd,
				})
			}
		case "Shared":
			if len(call.Args) == 0 {
				return true
			}
			if sp, ok := extractStringSpan(fset, call.Args[0]); ok {
				out = append(out, viewCall{
					Method:      "Shared",
					ShareKey:    &sp,
					MethodStart: mStart,
					MethodEnd:   mEnd,
				})
			}
		}
		return true
	})
	return out, fset
}

func extractStringSpan(fset *token.FileSet, expr ast.Expr) (viewNameSpan, bool) {
	bl, ok := expr.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return viewNameSpan{}, false
	}
	val, err := strconv.Unquote(bl.Value)
	if err != nil {
		return viewNameSpan{}, false
	}
	return viewNameSpan{
		Name:  val,
		Start: fset.Position(bl.Pos()).Offset,
		End:   fset.Position(bl.End()).Offset,
	}, true
}

func resolveDataMapArg(expr ast.Expr, localMaps map[string]*ast.CompositeLit) *ast.CompositeLit {
	if cl, ok := expr.(*ast.CompositeLit); ok {
		return cl
	}
	if ident, ok := expr.(*ast.Ident); ok && localMaps != nil {
		return localMaps[ident.Name]
	}
	return nil
}

// ViewCompletions suggests template paths inside View().Make/First/Exists/path.View, shared keys inside View().Shared/Share, and expected .tmpl variable keys inside View().Make data maps.
func ViewCompletions(path, src string, cat *indexer.Catalog, goOff int) []CompItem {
	if cat == nil {
		return nil
	}
	calls, fset := findViewCalls(path, src)
	for _, vc := range calls {
		for _, ns := range vc.Names {
			if goOff >= ns.Start && goOff <= ns.End {
				var names []string
				for k := range cat.Views {
					names = append(names, k)
				}
				sort.Strings(names)
				var out []CompItem
				for _, n := range names {
					detail := "resources/views/" + n
					doc := ""
					if vi := cat.ViewItems[n]; vi != nil {
						detail = vi.RelDisplay
						if len(vi.PackageLocs) > 0 {
							detail += " (overrides package view)"
						} else if vi.Source == "package" {
							detail += " (package view)"
						}
						if len(vi.UsedVars) > 0 {
							doc = "Variables: `." + strings.Join(vi.UsedVars, "`, `.`") + "`"
						}
					}
					out = append(out, CompItem{
						Label:  n,
						Detail: detail,
						Doc:    doc,
						Kind:   17, // File
					})
				}
				return out
			}
		}
		if vc.ShareKey != nil && goOff >= vc.ShareKey.Start && goOff <= vc.ShareKey.End {
			var keys []string
			for k := range cat.SharedViewVars {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var out []CompItem
			for _, k := range keys {
				vvs := cat.SharedViewVars[k]
				detail := "shared view variable"
				if len(vvs) > 0 {
					detail = fmt.Sprintf("%s (from %s)", vvs[0].ValueExpr, vvs[0].Controller)
				}
				out = append(out, CompItem{
					Label:  k,
					Detail: detail,
					Kind:   6, // Variable
				})
			}
			return out
		}
		// Data map key completion inside View().Make("view.tmpl", map[string]any{ "|" })
		if vc.DataMap != nil && fset != nil && len(vc.Names) > 0 {
			lbrace := fset.Position(vc.DataMap.Lbrace).Offset
			rbrace := fset.Position(vc.DataMap.Rbrace).Offset
			if goOff >= lbrace && goOff <= rbrace {
				var targetVi *indexer.ViewItem
				for _, ns := range vc.Names {
					if vi := cat.LookupView(ns.Name); vi != nil {
						targetVi = vi
						break
					}
				}
				if targetVi != nil && len(targetVi.UsedVars) > 0 {
					existing := map[string]bool{}
					for _, elt := range vc.DataMap.Elts {
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							if sp, ok := extractStringSpan(fset, kv.Key); ok && (goOff < sp.Start || goOff > sp.End) {
								existing[sp.Name] = true
							}
						}
					}
					directSet := map[string]bool{}
					for _, dv := range targetVi.DirectVars {
						directSet[dv] = true
					}
					var out []CompItem
					for _, uv := range targetVi.UsedVars {
						if existing[uv] {
							continue
						}
						origin := fmt.Sprintf("referenced in %s", targetVi.Name)
						if !directSet[uv] {
							origin = fmt.Sprintf("referenced in partial included by %s", targetVi.Name)
						}
						if _, isShared := cat.SharedViewVars[uv]; isShared {
							origin += " (also shared globally)"
						}
						out = append(out, CompItem{
							Label:  uv,
							Detail: origin,
							Doc:    fmt.Sprintf("Template context variable `.%s` expected by `%s`", uv, targetVi.Name),
							Kind:   5, // Field
						})
					}
					return out
				}
			}
		}
	}
	return nil
}

// ViewHover shows info about the referenced view template or shared view key.
func ViewHover(path, src string, cat *indexer.Catalog, goOff int) *HoverResult {
	if cat == nil {
		return nil
	}
	calls, fset := findViewCalls(path, src)
	for _, vc := range calls {
		for _, ns := range vc.Names {
			if goOff >= ns.Start && goOff <= ns.End {
				vi := cat.LookupView(ns.Name)
				if vi == nil {
					if loc, ok := cat.Views[ns.Name]; ok {
						return &HoverResult{Markdown: fmt.Sprintf("**View `%s`**\n\nFile: `resources/views/%s` (`%s`)", ns.Name, ns.Name, loc.File)}
					}
					return &HoverResult{Markdown: fmt.Sprintf("**View `%s`** *(not found in `resources/views/` or package views)*", ns.Name)}
				}
				var b strings.Builder
				fmt.Fprintf(&b, "**View `%s`** (%s)\n\n", vi.Name, vi.Source)
				fmt.Fprintf(&b, "- File: `%s` (`%s`)\n", vi.RelDisplay, vi.Loc.File)
				if vi.DefineName != "" {
					fmt.Fprintf(&b, "- Define: `{{ define %q }}`\n", vi.DefineName)
				}
				if len(vi.PackageLocs) > 0 {
					var overrides []string
					for _, pl := range vi.PackageLocs {
						overrides = append(overrides, fmt.Sprintf("`%s`", pl.File))
					}
					fmt.Fprintf(&b, "- Overrides package view(s): %s\n", strings.Join(overrides, ", "))
				}
				if len(vi.Includes) > 0 {
					var incs []string
					for _, inc := range vi.Includes {
						incs = append(incs, fmt.Sprintf("`%s`", inc.Target))
					}
					fmt.Fprintf(&b, "- Includes: %s\n", strings.Join(incs, ", "))
				}
				if len(vi.UsedVars) > 0 {
					fmt.Fprintf(&b, "- Template variables: `.%s`\n", strings.Join(vi.UsedVars, "`, `."))
				}
				return &HoverResult{Markdown: b.String()}
			}
		}
		if vc.ShareKey != nil && goOff >= vc.ShareKey.Start && goOff <= vc.ShareKey.End {
			key := vc.ShareKey.Name
			vvs := cat.SharedViewVars[key]
			var b strings.Builder
			fmt.Fprintf(&b, "**Shared View Variable `.%s`**\n\n", key)
			for _, vv := range vvs {
				fmt.Fprintf(&b, "- Shared as `%s` in `%s` (`%s:%d`)\n", vv.ValueExpr, vv.Controller, filepath.Base(vv.Loc.File), vv.Loc.Line+1)
			}
			var usedIn []string
			for _, vi := range cat.ViewItems {
				for _, uv := range vi.UsedVars {
					if uv == key {
						usedIn = append(usedIn, fmt.Sprintf("`%s`", vi.Name))
						break
					}
				}
			}
			if len(usedIn) > 0 {
				sort.Strings(usedIn)
				fmt.Fprintf(&b, "- Referenced in templates: %s\n", strings.Join(usedIn, ", "))
			}
			return &HoverResult{Markdown: b.String()}
		}
		if vc.Method == "GetShared" && goOff >= vc.MethodStart && goOff <= vc.MethodEnd {
			var b strings.Builder
			b.WriteString("**`facades.View().GetShared()`**\n\n")
			if len(cat.SharedViewVars) == 0 {
				b.WriteString("*(No global shared view variables registered via `View().Share`)*")
			} else {
				keys := make([]string, 0, len(cat.SharedViewVars))
				for k := range cat.SharedViewVars {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				b.WriteString("Registered shared variables:\n")
				for _, k := range keys {
					vvs := cat.SharedViewVars[k]
					if len(vvs) > 0 {
						fmt.Fprintf(&b, "- `.%s` = `%s` (%s)\n", k, vvs[0].ValueExpr, vvs[0].Controller)
					}
				}
			}
			return &HoverResult{Markdown: b.String()}
		}
		if vc.Method == "RegisteredViews" && goOff >= vc.MethodStart && goOff <= vc.MethodEnd {
			var names []string
			for k := range cat.ViewItems {
				names = append(names, k)
			}
			sort.Strings(names)
			var b strings.Builder
			fmt.Fprintf(&b, "**`facades.View().RegisteredViews()`** (%d templates)\n\n", len(names))
			for _, n := range names {
				vi := cat.ViewItems[n]
				fmt.Fprintf(&b, "- `%s` (%s)\n", n, vi.Source)
			}
			return &HoverResult{Markdown: b.String()}
		}
		if vc.DataMap != nil && fset != nil && len(vc.Names) > 0 {
			for _, elt := range vc.DataMap.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				sp, ok := extractStringSpan(fset, kv.Key)
				if !ok || goOff < sp.Start || goOff > sp.End {
					continue
				}
				vi := cat.LookupView(vc.Names[0].Name)
				if vi != nil {
					return &HoverResult{
						Markdown: fmt.Sprintf("**View Context Key `.%s`**\n\nPassed to template `%s` (`%s`)", sp.Name, vi.Name, vi.RelDisplay),
					}
				}
			}
		}
	}
	return nil
}

// ViewLocations returns all definition locations for a View call (active template + overridden package views, or template var occurrences).
func ViewLocations(path, src string, cat *indexer.Catalog, goOff int) []indexer.Location {
	if cat == nil {
		return nil
	}
	calls, fset := findViewCalls(path, src)
	var locs []indexer.Location
	for _, vc := range calls {
		for _, ns := range vc.Names {
			if goOff >= ns.Start && goOff <= ns.End {
				if vi := cat.LookupView(ns.Name); vi != nil {
					if vi.DefineLoc.Valid() {
						locs = append(locs, vi.DefineLoc)
					} else if vi.Loc.Valid() {
						locs = append(locs, vi.Loc)
					}
					for _, pl := range vi.PackageLocs {
						if pl.Valid() {
							locs = append(locs, pl)
						}
					}
					return locs
				}
				if loc, ok := cat.Views[ns.Name]; ok {
					return []indexer.Location{loc}
				}
			}
		}
		if vc.ShareKey != nil && goOff >= vc.ShareKey.Start && goOff <= vc.ShareKey.End {
			key := vc.ShareKey.Name
			if vc.Method == "Shared" {
				for _, vv := range cat.SharedViewVars[key] {
					if vv.Loc.Valid() {
						locs = append(locs, vv.Loc)
					}
				}
				if len(locs) > 0 {
					return locs
				}
			}
			// Jump to templates referencing .key
			for _, vi := range cat.ViewItems {
				locs = append(locs, findTemplateVarOccurrencesInFile(vi.Loc.File, key)...)
			}
			return locs
		}
		if vc.DataMap != nil && fset != nil {
			for _, elt := range vc.DataMap.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				sp, ok := extractStringSpan(fset, kv.Key)
				if !ok || goOff < sp.Start || goOff > sp.End {
					continue
				}
				for _, ns := range vc.Names {
					if vi := cat.LookupView(ns.Name); vi != nil {
						locs = append(locs, findTemplateVarOccurrencesInFile(vi.Loc.File, sp.Name)...)
						for _, inc := range vi.Includes {
							if child := cat.LookupView(inc.Target); child != nil {
								locs = append(locs, findTemplateVarOccurrencesInFile(child.Loc.File, sp.Name)...)
							}
						}
					}
				}
				if len(locs) > 0 {
					return locs
				}
			}
		}
	}
	return nil
}

// ViewDefinition jumps from View().Make("...") to resources/views/<name> or package view.
func ViewDefinition(path, src string, cat *indexer.Catalog, goOff int) *DefResult {
	locs := ViewLocations(path, src, cat, goOff)
	if len(locs) > 0 {
		return &DefResult{Loc: locs[0]}
	}
	return nil
}

// ViewDiagnostics warns on missing templates and unused data keys in View().Make/First/Exists.
func ViewDiagnostics(path, src string, cat *indexer.Catalog) []Diag {
	if cat == nil || (len(cat.Views) == 0 && len(cat.ViewItems) == 0) {
		return nil
	}
	calls, fset := findViewCalls(path, src)
	var out []Diag
	for _, vc := range calls {
		switch vc.Method {
		case "Make", "PathView", "Exists":
			if len(vc.Names) == 0 {
				continue
			}
			ns := vc.Names[0]
			vi := cat.LookupView(ns.Name)
			if vi == nil {
				if _, ok := cat.Views[ns.Name]; !ok {
					sev := 2 // Warning
					if vc.Method == "Exists" {
						sev = 4 // Hint for Exists("...")
					}
					out = append(out, Diag{
						Start:    ns.Start,
						End:      ns.End,
						Severity: sev,
						Message:  fmt.Sprintf("View template '%s' not found in resources/views/ or registered package views", ns.Name),
					})
					continue
				}
			}
			if vc.Method == "Make" && vc.DataMap != nil && fset != nil {
				out = append(out, checkUnusedViewDataMap(vc.Names[0].Name, vi, vc.DataMap, fset, cat)...)
			}
		case "First":
			anyFound := false
			var firstValid *indexer.ViewItem
			for _, ns := range vc.Names {
				if vi := cat.LookupView(ns.Name); vi != nil {
					anyFound = true
					if firstValid == nil {
						firstValid = vi
					}
				} else if _, ok := cat.Views[ns.Name]; ok {
					anyFound = true
				}
			}
			if !anyFound && len(vc.Names) > 0 {
				out = append(out, Diag{
					Start:    vc.Names[0].Start,
					End:      vc.Names[len(vc.Names)-1].End,
					Severity: 2,
					Message:  "None of the fallback view templates in View().First(...) exist in resources/views/ or package views",
				})
			} else if firstValid != nil && vc.DataMap != nil && fset != nil {
				out = append(out, checkUnusedViewDataMap(firstValid.Name, firstValid, vc.DataMap, fset, cat)...)
			}
		case "Shared":
			if vc.ShareKey != nil && len(cat.SharedViewVars) > 0 {
				if _, ok := cat.SharedViewVars[vc.ShareKey.Name]; !ok {
					out = append(out, Diag{
						Start:    vc.ShareKey.Start,
						End:      vc.ShareKey.End,
						Severity: 4,
						Message:  fmt.Sprintf("Shared view key '%s' is not registered via facades.View().Share()", vc.ShareKey.Name),
					})
				}
			}
		}
	}
	return out
}

func checkUnusedViewDataMap(viewName string, vi *indexer.ViewItem, dataMap *ast.CompositeLit, fset *token.FileSet, cat *indexer.Catalog) []Diag {
	var filePath string
	usedSet := map[string]bool{}
	passesDot := false
	if vi != nil {
		filePath = vi.Loc.File
		passesDot = vi.PassesDot
		for _, uv := range vi.UsedVars {
			usedSet[uv] = true
		}
	} else if loc, ok := cat.Views[viewName]; ok {
		filePath = loc.File
	}
	if passesDot || filePath == "" {
		return nil
	}
	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	tplSrc := string(b)
	var out []Diag
	for _, elt := range dataMap.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		bl, ok := kv.Key.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			continue
		}
		k, err := strconv.Unquote(bl.Value)
		if err != nil || k == "" {
			continue
		}
		if !usedSet[k] && !strings.Contains(tplSrc, "."+k) {
			out = append(out, Diag{
				Start:    fset.Position(bl.Pos()).Offset,
				End:      fset.Position(bl.End()).Offset,
				Severity: 4, // Hint
				Message:  fmt.Sprintf("View data key '%s' is not referenced in %s", k, viewName),
			})
		}
	}
	// Also check if template expects required direct variables that were omitted from a non-empty map literal
	if vi != nil && len(dataMap.Elts) > 0 {
		passedKeys := map[string]bool{}
		for _, elt := range dataMap.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if bl, ok := kv.Key.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					if k, err := strconv.Unquote(bl.Value); err == nil {
						passedKeys[k] = true
					}
				}
			}
		}
		var missingVars []string
		for _, uv := range vi.UsedVars {
			if passedKeys[uv] || uv == "csrf_token" {
				continue
			}
			if _, isShared := cat.SharedViewVars[uv]; isShared {
				continue
			}
			missingVars = append(missingVars, "."+uv)
		}
		if len(missingVars) > 0 {
			sort.Strings(missingVars)
			out = append(out, Diag{
				Start:    fset.Position(dataMap.Lbrace).Offset,
				End:      fset.Position(dataMap.Lbrace).Offset + 1,
				Severity: 4, // Hint
				Message:  fmt.Sprintf("Template '%s' also references variable(s) not in this map: %s", viewName, strings.Join(missingVars, ", ")),
			})
		}
	}
	return out
}

// ViewCodeActions offers Quick Fixes to scaffold a missing template in resources/views/ or fill missing template variables into a View().Make data map.
func ViewCodeActions(path, src string, cat *indexer.Catalog, goOff int) []CodeAction {
	if cat == nil || cat.Root == "" {
		return nil
	}
	calls, fset := findViewCalls(path, src)
	var actions []CodeAction
	for _, vc := range calls {
		for _, ns := range vc.Names {
			if goOff >= ns.Start && goOff <= ns.End {
				if cat.LookupView(ns.Name) != nil {
					continue
				}
				if _, ok := cat.Views[ns.Name]; ok {
					continue
				}
				relName := ns.Name
				if filepath.Ext(relName) == "" {
					relName += ".tmpl"
				}
				targetFile := filepath.Join(cat.Root, "resources", "views", filepath.FromSlash(relName))
				var bodyLines []string
				bodyLines = append(bodyLines, fmt.Sprintf("{{ define %q }}", relName))
				bodyLines = append(bodyLines, "<div>")
				if vc.DataMap != nil {
					for _, elt := range vc.DataMap.Elts {
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							if bl, ok := kv.Key.(*ast.BasicLit); ok && bl.Kind == token.STRING {
								if k, err := strconv.Unquote(bl.Value); err == nil && k != "" {
									bodyLines = append(bodyLines, fmt.Sprintf("  <p>{{ .%s }}</p>", k))
								}
							}
						}
					}
				}
				bodyLines = append(bodyLines, "</div>")
				bodyLines = append(bodyLines, "{{ end }}\n")
				actions = append(actions, CodeAction{
					Title: fmt.Sprintf("Create view template 'resources/views/%s'", relName),
					Kind:  "quickfix",
					Edits: []TextEdit{
						{
							File:    targetFile,
							Start:   0,
							End:     0,
							NewText: strings.Join(bodyLines, "\n"),
						},
					},
				})
			}
		}

		// Quick Fix: Fill missing template variables into View().Make / First data map
		if vc.DataMap != nil && fset != nil && len(vc.Names) > 0 {
			lbrace := fset.Position(vc.DataMap.Lbrace).Offset
			rbrace := fset.Position(vc.DataMap.Rbrace).Offset
			onCall := (goOff >= vc.Names[0].Start && goOff <= vc.Names[len(vc.Names)-1].End) || (goOff >= lbrace && goOff <= rbrace)
			if onCall {
				vi := cat.LookupView(vc.Names[0].Name)
				if vi != nil && len(vi.UsedVars) > 0 {
					passed := map[string]bool{}
					for _, elt := range vc.DataMap.Elts {
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							if sp, ok := extractStringSpan(fset, kv.Key); ok {
								passed[sp.Name] = true
							}
						}
					}
					var missing []string
					for _, uv := range vi.UsedVars {
						if passed[uv] || uv == "csrf_token" {
							continue
						}
						if _, isShared := cat.SharedViewVars[uv]; isShared {
							continue
						}
						missing = append(missing, uv)
					}
					if len(missing) > 0 {
						sort.Strings(missing)
						var ins strings.Builder
						for _, mKey := range missing {
							fmt.Fprintf(&ins, "\n\t\t%q: \"\",", mKey)
						}
						actions = append(actions, CodeAction{
							Title: fmt.Sprintf("Fill missing template variables (%s)", strings.Join(missing, ", ")),
							Kind:  "quickfix",
							Edits: []TextEdit{
								{
									File:    path,
									Start:   lbrace + 1,
									End:     lbrace + 1,
									NewText: ins.String(),
								},
							},
						})
					}
				}
			}
		}
	}
	return actions
}

// InlayHintItem represents an inline editor hint at a byte offset.
type InlayHintItem struct {
	Offset int
	Label  string
}

// ViewInlayHints returns inlay hints on View().Make calls in Go files when a template has unpassed variables or overrides a package view.
func ViewInlayHints(path, src string, cat *indexer.Catalog) []InlayHintItem {
	if cat == nil {
		return nil
	}
	calls, fset := findViewCalls(path, src)
	var hints []InlayHintItem
	for _, vc := range calls {
		if vc.Method != "Make" || len(vc.Names) == 0 {
			continue
		}
		ns := vc.Names[0]
		vi := cat.LookupView(ns.Name)
		if vi == nil {
			continue
		}
		if len(vi.PackageLocs) > 0 {
			hints = append(hints, InlayHintItem{
				Offset: ns.End,
				Label:  " [overrides package view]",
			})
		}
		if vc.DataMap != nil && fset != nil && len(vi.UsedVars) > 0 {
			passed := map[string]bool{}
			for _, elt := range vc.DataMap.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if sp, ok := extractStringSpan(fset, kv.Key); ok {
						passed[sp.Name] = true
					}
				}
			}
			var missing []string
			for _, uv := range vi.UsedVars {
				if !passed[uv] && uv != "csrf_token" {
					if _, isShared := cat.SharedViewVars[uv]; !isShared {
						missing = append(missing, "."+uv)
					}
				}
			}
			if len(missing) > 0 && len(missing) <= 4 {
				hints = append(hints, InlayHintItem{
					Offset: ns.End,
					Label:  fmt.Sprintf(" (missing: %s)", strings.Join(missing, ", ")),
				})
			}
		}
	}
	return hints
}

// IsTemplateFile reports whether path is an HTML/Go template file.
func IsTemplateFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".tmpl" || ext == ".html" || ext == ".gohtml"
}

func resolveViewVarsForTemplate(path string, cat *indexer.Catalog) []indexer.ViewVar {
	if cat == nil {
		return nil
	}
	slash := filepath.ToSlash(path)
	var keys []string
	if vi := cat.ViewForPath(path); vi != nil {
		keys = append(keys, vi.Name, strings.TrimSuffix(vi.Name, filepath.Ext(vi.Name)))
		if vi.DefineName != "" {
			keys = append(keys, vi.DefineName)
		}
	}
	if idx := strings.Index(slash, "/resources/views/"); idx >= 0 {
		rel := slash[idx+len("/resources/views/"):]
		keys = append(keys, rel, strings.TrimSuffix(rel, filepath.Ext(rel)))
	}
	for _, pkgDir := range cat.PackageViewDirs {
		pkgSlash := filepath.ToSlash(pkgDir) + "/"
		if strings.HasPrefix(slash, pkgSlash) {
			rel := strings.TrimPrefix(slash, pkgSlash)
			keys = append(keys, rel, strings.TrimSuffix(rel, filepath.Ext(rel)))
		}
	}
	base := filepath.Base(slash)
	keys = append(keys, base, strings.TrimSuffix(base, filepath.Ext(base)))

	seen := map[string]bool{}
	var out []indexer.ViewVar
	for _, k := range keys {
		for _, vv := range cat.ViewVars[k] {
			id := vv.Key + "@" + vv.Controller + "@" + vv.Inherited
			if !seen[id] {
				seen[id] = true
				out = append(out, vv)
			}
		}
	}
	// Include global shared variables from facades.View().Share(key, val)
	sharedKeys := make([]string, 0, len(cat.SharedViewVars))
	for sk := range cat.SharedViewVars {
		sharedKeys = append(sharedKeys, sk)
	}
	sort.Strings(sharedKeys)
	for _, sk := range sharedKeys {
		for _, sv := range cat.SharedViewVars[sk] {
			id := sv.Key + "@shared@" + sv.Controller
			if !seen[id] {
				seen[id] = true
				out = append(out, sv)
			}
		}
	}
	return out
}

func isInsideTemplateAction(src string, off int) bool {
	if off < 0 || off > len(src) {
		return false
	}
	openIdx := strings.LastIndex(src[:off], "{{")
	if openIdx < 0 {
		return false
	}
	closeBefore := strings.LastIndex(src[:off], "}}")
	return openIdx > closeBefore
}

func currentTemplateActionPrefix(src string, off int) string {
	if off < 0 || off > len(src) {
		return ""
	}
	openIdx := strings.LastIndex(src[:off], "{{")
	if openIdx < 0 {
		return ""
	}
	inner := src[openIdx+2 : off]
	return strings.TrimPrefix(inner, "-")
}

var (
	includeCompletionPrefixRe = regexp.MustCompile(`^\s*(?:template|block)\s+"([^"]*)$`)
	nestedFieldPrefixRe       = regexp.MustCompile(`(?:^|[\s(|,=])(\$?[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*|\.[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\.([A-Za-z0-9_]*)$`)
	pipePrefixRe              = regexp.MustCompile(`\|\s*[A-Za-z0-9_]*$`)
	localVarDeclRe            = regexp.MustCompile(`\{\{-?\s*(?:range\s+|with\s+|if\s+)?(\$[A-Za-z_][A-Za-z0-9_]*)(?:\s*,\s*(\$[A-Za-z_][A-Za-z0-9_]*))?\s*:=\s*([^}\n]+?)\s*-?\}\}`)
	htmlRouteAttrPrefixRe     = regexp.MustCompile(`(?i)(?:href|action|hx-get|hx-post|hx-put|hx-delete|hx-patch)\s*=\s*["'](/[^"'{}]*)?$`)
	jsFetchRoutePrefixRe      = regexp.MustCompile(`(?i)(?:fetch|axios\.(?:get|post|put|patch|delete))\s*\(\s*["'\x60](/[^"'\x60{}]*)?$`)
	htmlAssetAttrPrefixRe     = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["'](/[^"'{}]*)?$`)
	tmplActionRe              = regexp.MustCompile(`(?s)\{\{-?(.*?)-?\}\}`)
	tmplDefineLeadRe          = regexp.MustCompile(`^\s*define\s+"([^"]+)"`)
	tmplIncludeLeadRe         = regexp.MustCompile(`^\s*(?:template|block)\s+"([^"]+)"`)
	tmplDotVarRe              = regexp.MustCompile(`(?:^|[\s(|,=:$])(\$)?\.([A-Za-z_][A-Za-z0-9_]*)`)
	tmplIdentRe               = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	tmplDefineTrimmedRe       = regexp.MustCompile(`^define\s+"([^"]+)"`)
	tmplIncludeTrimmedRe      = regexp.MustCompile(`^(template|block)\s+"([^"]+)"(?:\s+(.+))?`)
	tmplScopeTrimmedRe        = regexp.MustCompile(`^(range|with)\s+(.+)$`)
	tmplLocalVarTrimmedRe     = regexp.MustCompile(`^(\$[A-Za-z_][A-Za-z0-9_]*)\s*:=\s*(.+)$`)
	htmlStaticAssetRe         = regexp.MustCompile(`(?i)\b(?:src|href)\s*=\s*["'](/[A-Za-z0-9_./-]+)["']`)
	tmplIncludeActionStartRe  = regexp.MustCompile(`^\{\{-?\s*(define|template|block)\s+"([^"]+)"`)
	tmplIncludeAnyActionRe    = regexp.MustCompile(`\{\{-?\s*(?:define|template|block)\s+"([^"]+)"`)
)

type localTemplateVar struct {
	Name   string
	Expr   string
	GoType string
	Offset int
}

func extractLocalTemplateVars(src string, limitOff int, vars []indexer.ViewVar, cat *indexer.Catalog) []localTemplateVar {
	if limitOff > len(src) {
		limitOff = len(src)
	}
	var out []localTemplateVar
	seen := map[string]bool{}
	for _, m := range localVarDeclRe.FindAllStringSubmatchIndex(src[:limitOff], -1) {
		actText := src[m[0]:m[1]]
		isRange := strings.Contains(actText, "range ")
		rhs := strings.TrimSpace(src[m[6]:m[7]])
		rhsField := strings.TrimPrefix(strings.TrimPrefix(rhs, "$."), ".")
		inferredType := resolveExprChainGoType(rhsField, "", vars, out, cat)
		if isRange {
			inferredType = strings.TrimPrefix(inferredType, "[]")
		}
		if m[4] >= 0 && m[5] >= 0 {
			// range $idx, $item := .items
			idxName := src[m[2]:m[3]]
			itemName := src[m[4]:m[5]]
			if !seen[idxName] {
				seen[idxName] = true
				out = append(out, localTemplateVar{Name: idxName, Expr: "index of " + rhs, Offset: m[2]})
			}
			if !seen[itemName] {
				seen[itemName] = true
				out = append(out, localTemplateVar{Name: itemName, Expr: rhs, GoType: inferredType, Offset: m[4]})
			}
		} else if m[2] >= 0 && m[3] >= 0 {
			vName := src[m[2]:m[3]]
			if !seen[vName] {
				seen[vName] = true
				out = append(out, localTemplateVar{Name: vName, Expr: rhs, GoType: inferredType, Offset: m[2]})
			}
		}
	}
	return out
}

// resolveScopedDotGoType determines if off is inside an active {{ range .items }} or {{ with .user }} block that rebinds `.`.
func resolveScopedDotGoType(src string, off int, vars []indexer.ViewVar, locals []localTemplateVar, cat *indexer.Catalog) string {
	if off > len(src) {
		off = len(src)
	}
	type scopeFrame struct {
		kw      string
		dotType string
	}
	var stack []scopeFrame
	matches := tmplActionRe.FindAllStringSubmatchIndex(src[:off], -1)
	for _, m := range matches {
		body := strings.TrimSpace(src[m[2]:m[3]])
		if strings.HasPrefix(body, "/*") {
			continue
		}
		if bm := blockOpenKeywordRe.FindStringSubmatch(body); bm != nil {
			kw := bm[1]
			dotType := ""
			if kw == "range" || kw == "with" {
				rest := strings.TrimSpace(body[len(kw):])
				if idx := strings.Index(rest, ":="); idx >= 0 {
					rest = strings.TrimSpace(rest[idx+2:])
				}
				fields := strings.Fields(rest)
				if len(fields) > 0 {
					expr := fields[0]
					curDot := ""
					if len(stack) > 0 {
						curDot = stack[len(stack)-1].dotType
					}
					dotType = resolveExprChainGoType(expr, curDot, vars, locals, cat)
					if kw == "range" {
						dotType = strings.TrimPrefix(dotType, "[]")
					}
				}
			} else if len(stack) > 0 {
				dotType = stack[len(stack)-1].dotType
			}
			stack = append(stack, scopeFrame{kw: kw, dotType: dotType})
		} else if blockEndKeywordRe.MatchString(body) {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].dotType != "" {
			return stack[i].dotType
		}
	}
	return ""
}

// resolveExprChainGoType resolves a single or multi-segment chain like ".profile.User" or "$u.Province" to its Go struct/model type.
func resolveExprChainGoType(chainExpr, scopedDotType string, vars []indexer.ViewVar, locals []localTemplateVar, cat *indexer.Catalog) string {
	if chainExpr == "" {
		return ""
	}
	isRootDollar := strings.HasPrefix(chainExpr, "$.")
	clean := strings.TrimPrefix(strings.TrimPrefix(chainExpr, "$."), ".")
	parts := strings.Split(clean, ".")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	curType := ""
	first := parts[0]
	if strings.HasPrefix(first, "$") {
		for _, lv := range locals {
			if lv.Name == first && lv.GoType != "" {
				curType = strings.TrimPrefix(lv.GoType, "[]")
				break
			}
		}
	} else {
		if !isRootDollar && scopedDotType != "" && cat != nil {
			if ft, _, ok := stepStructOrModelField(scopedDotType, first, cat); ok {
				curType = strings.TrimPrefix(ft, "[]")
			}
		}
		if curType == "" {
			for _, vv := range vars {
				if vv.Key == first && vv.GoType != "" {
					curType = strings.TrimPrefix(vv.GoType, "[]")
					break
				}
			}
		}
	}
	for i := 1; i < len(parts) && curType != "" && cat != nil; i++ {
		ft, _, ok := stepStructOrModelField(curType, parts[i], cat)
		if !ok {
			return ""
		}
		curType = strings.TrimPrefix(ft, "[]")
	}
	return curType
}

func stepStructOrModelField(holderType, field string, cat *indexer.Catalog) (fieldGoType string, fieldLoc indexer.Location, found bool) {
	if cat == nil || holderType == "" || field == "" {
		return "", indexer.Location{}, false
	}
	base := strings.TrimPrefix(holderType, "[]")
	if tbl := cat.Model(base); tbl != nil {
		for _, c := range tbl.Columns {
			if c.GoName == field {
				return cleanTypeRef(c.GoType), c.Loc, true
			}
		}
		for _, r := range tbl.Relations {
			if r.Name == field {
				return r.TargetModel, r.Loc, true
			}
		}
	}
	if st := cat.Struct(base); st != nil {
		for _, f := range st.Fields {
			if f.GoName == field {
				return cleanTypeRef(f.GoType), f.Loc, true
			}
		}
	}
	return "", indexer.Location{}, false
}

func cleanTypeRef(raw string) string {
	s := strings.TrimSpace(raw)
	isSlice := false
	if strings.HasPrefix(s, "[]") {
		isSlice = true
		s = strings.TrimSpace(s[2:])
	}
	s = strings.TrimPrefix(s, "*")
	if idx := strings.LastIndexByte(s, '.'); idx >= 0 {
		s = s[idx+1:]
	}
	if isSlice && s != "" {
		return "[]" + s
	}
	return s
}

var (
	tmplKeywords = []struct {
		Label  string
		Detail string
	}{
		{"define", "Define a named template block: {{ define \"name\" }}...{{ end }}"},
		{"template", "Include and execute a named template: {{ template \"name\" . }}"},
		{"block", "Define and execute a replaceable block: {{ block \"name\" . }}...{{ end }}"},
		{"if", "Conditional block: {{ if .condition }}...{{ end }}"},
		{"else", "Else branch in if/with/range block"},
		{"range", "Iterate over slice/map/channel: {{ range .items }}...{{ end }}"},
		{"with", "Set dot (.) to pipeline value if non-empty: {{ with .user }}...{{ end }}"},
		{"end", "Close define, if, range, with, or block action"},
		{"nil", "Untyped Go nil constant"},
		{"break", "Exit innermost range loop early"},
		{"continue", "Skip to next iteration of innermost range loop"},
	}
	tmplBuiltins = []struct {
		Label  string
		Detail string
	}{
		{"len", "built-in: len(item) -> int length of slice/map/string"},
		{"index", "built-in: index(collection, keyOrIdx...) -> element"},
		{"slice", "built-in: slice(item, low, high...) -> sub-slice"},
		{"printf", "built-in: fmt.Sprintf(format, args...)"},
		{"print", "built-in: fmt.Sprint(args...)"},
		{"println", "built-in: fmt.Sprintln(args...)"},
		{"and", "built-in: boolean AND (returns first empty arg or last arg)"},
		{"or", "built-in: boolean OR (returns first non-empty arg or last arg)"},
		{"not", "built-in: boolean negation of single argument"},
		{"eq", "built-in: arg1 == arg2 (or arg1 == arg2 || arg1 == arg3 ...)"},
		{"ne", "built-in: arg1 != arg2"},
		{"lt", "built-in: arg1 < arg2"},
		{"le", "built-in: arg1 <= arg2"},
		{"gt", "built-in: arg1 > arg2"},
		{"ge", "built-in: arg1 >= arg2"},
		{"html", "built-in: escape HTML special characters"},
		{"js", "built-in: escape JavaScript special characters"},
		{"urlquery", "built-in: escape URL query string"},
		{"call", "built-in: call a function value with arguments"},
	}
)

// TemplateCompletions suggests sub-templates, context/shared/inherited variables, nested struct fields, local $vars, custom FuncMap functions, and HTML routes/assets.
func TemplateCompletions(path, src string, cat *indexer.Catalog, off int) []CompItem {
	if !IsTemplateFile(path) {
		return nil
	}
	if !isInsideTemplateAction(src, off) {
		return htmlRouteAndAssetCompletions(src, off, cat)
	}
	prefix := currentTemplateActionPrefix(src, off)

	// 1. Inside {{ template "|" }} or {{ block "|" }}
	if includeCompletionPrefixRe.MatchString(prefix) {
		if cat == nil {
			return nil
		}
		seen := map[string]bool{}
		var names []string
		for k := range cat.Views {
			names = append(names, k)
			seen[k] = true
		}
		for _, vi := range cat.ViewItems {
			if vi.DefineName != "" && !seen[vi.DefineName] {
				names = append(names, vi.DefineName)
				seen[vi.DefineName] = true
			}
			for _, d := range vi.Defines {
				if d.Name != "" && !seen[d.Name] {
					names = append(names, d.Name)
					seen[d.Name] = true
				}
			}
		}
		sort.Strings(names)
		var out []CompItem
		for _, n := range names {
			detail := "resources/views/" + n
			if vi := cat.LookupView(n); vi != nil {
				detail = vi.RelDisplay
			}
			out = append(out, CompItem{
				Label:  n,
				Detail: detail,
				Kind:   17, // File
			})
		}
		return out
	}

	vars := resolveViewVarsForTemplate(path, cat)
	locals := extractLocalTemplateVars(src, off, vars, cat)
	scopedDotType := resolveScopedDotGoType(src, off, vars, locals, cat)

	// 1b. Immediately after pipe `|`: prioritize custom FuncMap functions and Go built-ins
	if pipePrefixRe.MatchString(prefix) {
		return templateFuncCompletions(cat)
	}

	// 2. Multi-level struct/model field completion: {{ .user.| }}, {{ .profile.User.| }}, or {{ $item.| }}
	if m := nestedFieldPrefixRe.FindStringSubmatch(prefix); m != nil && cat != nil {
		goType := resolveExprChainGoType(m[1], scopedDotType, vars, locals, cat)
		if goType != "" {
			if items := structFieldCompletions(strings.TrimPrefix(goType, "[]"), cat); len(items) > 0 {
				return items
			}
		}
	}

	hasDotPrefix := off > 0 && src[off-1] == '.'
	hasRootDollarDot := off >= 2 && src[off-2:off] == "$."
	hasDollarPrefix := off > 0 && src[off-1] == '$'

	// 3. Local template variable completion when typing $
	if hasDollarPrefix {
		var out []CompItem
		for _, lv := range locals {
			lbl := strings.TrimPrefix(lv.Name, "$")
			detail := lv.Expr
			if lv.GoType != "" {
				detail += " (" + lv.GoType + ")"
			}
			out = append(out, CompItem{
				Label:  lbl,
				Detail: detail,
				Kind:   6, // Variable
			})
		}
		return out
	}

	var out []CompItem
	seen := map[string]bool{}

	// If inside a {{ range }} or {{ with }} block that rebinds `.` (and not typing `$.`), include the scoped struct's fields first!
	if scopedDotType != "" && !hasRootDollarDot && cat != nil {
		for _, sf := range structFieldCompletions(scopedDotType, cat) {
			lbl := "." + sf.Label
			if hasDotPrefix {
				lbl = sf.Label
			}
			seen[sf.Label] = true
			out = append(out, CompItem{
				Label:  lbl,
				Detail: fmt.Sprintf("%s [scoped . in %s]", sf.Detail, scopedDotType),
				Kind:   5,
			})
		}
	}

	for _, vv := range vars {
		if seen[vv.Key] {
			continue
		}
		seen[vv.Key] = true
		lbl := "." + vv.Key
		if hasDotPrefix {
			lbl = vv.Key
		}
		detail := vv.ValueExpr
		if vv.GoType != "" {
			detail += " [" + vv.GoType + "]"
		}
		if vv.Shared {
			detail += " (shared via View().Share)"
		} else if vv.Inherited != "" {
			detail += fmt.Sprintf(" (via %s ← %s)", vv.Inherited, vv.Controller)
		} else if vv.Controller != "" {
			detail = fmt.Sprintf("%s (from %s)", detail, vv.Controller)
		}
		out = append(out, CompItem{
			Label:  lbl,
			Detail: detail,
			Kind:   6, // Variable
		})
	}
	if !seen["csrf_token"] {
		lbl := ".csrf_token"
		if hasDotPrefix {
			lbl = "csrf_token"
		}
		out = append(out, CompItem{
			Label:  lbl,
			Detail: "Goravel CSRF Token (VerifyCsrfToken middleware)",
			Kind:   6,
		})
	}

	// If not immediately after '.', also include local $vars, template keywords, custom FuncMap functions, and built-in functions
	if !hasDotPrefix {
		for _, lv := range locals {
			out = append(out, CompItem{
				Label:  lv.Name,
				Detail: "local: " + lv.Expr,
				Kind:   6,
			})
		}
		for _, kw := range tmplKeywords {
			out = append(out, CompItem{
				Label:  kw.Label,
				Detail: kw.Detail,
				Kind:   14, // Keyword
			})
		}
		out = append(out, templateFuncCompletions(cat)...)
	}
	return out
}

func templateFuncCompletions(cat *indexer.Catalog) []CompItem {
	var out []CompItem
	seen := map[string]bool{}
	if cat != nil && len(cat.TemplateFuncs) > 0 {
		names := make([]string, 0, len(cat.TemplateFuncs))
		for k := range cat.TemplateFuncs {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, n := range names {
			fn := cat.TemplateFuncs[n]
			seen[n] = true
			out = append(out, CompItem{
				Label:  fn.Name,
				Detail: fmt.Sprintf("custom FuncMap: %s (%s:%d)", fn.Signature, filepath.Base(fn.Loc.File), fn.Loc.Line+1),
				Kind:   3, // Function
			})
		}
	}
	for _, fn := range tmplBuiltins {
		if seen[fn.Label] {
			continue
		}
		out = append(out, CompItem{
			Label:  fn.Label,
			Detail: fn.Detail,
			Kind:   3, // Function
		})
	}
	return out
}

func structFieldCompletions(goType string, cat *indexer.Catalog) []CompItem {
	var out []CompItem
	seen := map[string]bool{}
	if st := cat.Struct(goType); st != nil {
		for _, f := range st.Fields {
			if f.GoName == "" || seen[f.GoName] {
				continue
			}
			seen[f.GoName] = true
			detail := f.GoType
			if f.Col != "" {
				detail += " (column: " + f.Col + ")"
			}
			out = append(out, CompItem{
				Label:  f.GoName,
				Detail: detail,
				Kind:   5, // Field
			})
		}
	}
	if tbl := cat.Model(goType); tbl != nil {
		for _, c := range tbl.Columns {
			if c.GoName == "" || seen[c.GoName] {
				continue
			}
			seen[c.GoName] = true
			out = append(out, CompItem{
				Label:  c.GoName,
				Detail: fmt.Sprintf("%s (column: %s)", c.GoType, c.Name),
				Kind:   5,
			})
		}
		for _, r := range tbl.Relations {
			if r.Name == "" || seen[r.Name] {
				continue
			}
			seen[r.Name] = true
			out = append(out, CompItem{
				Label:  r.Name,
				Detail: fmt.Sprintf("relation -> models.%s", r.TargetModel),
				Kind:   5,
			})
		}
	}
	return out
}

// TemplateHover displays controller origin, value expression, sub-template info, custom FuncMap signature, struct field info, or HTML route/asset info.
func TemplateHover(path, src string, cat *indexer.Catalog, off int) *HoverResult {
	if !IsTemplateFile(path) {
		return nil
	}
	if !isInsideTemplateAction(src, off) {
		return htmlRouteAndAssetHover(src, off, cat)
	}

	// 1. Check if hovering over {{ template "name" }} or {{ define "name" }} or {{ block "name" }}
	if directive, targetName, nStart, nEnd := templateDirectiveSpanAtOffset(src, off); directive != "" && targetName != "" && off <= nEnd+1 && cat != nil {
		vi := cat.LookupView(targetName)
		if directive == "define" {
			if vi == nil {
				vi = cat.ViewForPath(path)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "**Template Definition `%s`**\n\n", targetName)
			if vi != nil {
				fmt.Fprintf(&b, "- File: `%s`\n", vi.RelDisplay)
				if len(vi.Callers) > 0 {
					b.WriteString("- Rendered/Referenced by:\n")
					for _, c := range vi.Callers {
						lbl := c.Controller
						if c.Route != "" {
							lbl += " (`" + c.Route + "`)"
						}
						fmt.Fprintf(&b, "  - `%s` in `%s` (`%s:%d`)\n", c.Method, lbl, filepath.Base(c.Loc.File), c.Loc.Line+1)
					}
				}
			}
			return &HoverResult{Markdown: b.String(), Start: nStart, End: nEnd}
		}
		if vi == nil {
			return &HoverResult{Markdown: fmt.Sprintf("**Included Template `%s`** *(not found)*", targetName), Start: nStart, End: nEnd}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "**Included Template `%s`** (%s)\n\n", vi.Name, vi.Source)
		fmt.Fprintf(&b, "- File: `%s` (`%s`)\n", vi.RelDisplay, vi.Loc.File)
		if len(vi.UsedVars) > 0 {
			var uvars []string
			for _, uv := range vi.UsedVars {
				uvars = append(uvars, "`."+uv+"`")
			}
			fmt.Fprintf(&b, "- Uses variables: %s\n", strings.Join(uvars, ", "))
		}
		if raw, err := os.ReadFile(vi.Loc.File); err == nil {
			preview := strings.TrimSpace(string(raw))
			if len(preview) > 400 {
				preview = preview[:400] + "\n..."
			}
			fmt.Fprintf(&b, "\n```html\n%s\n```", preview)
		}
		return &HoverResult{Markdown: b.String(), Start: nStart, End: nEnd}
	}

	vars := resolveViewVarsForTemplate(path, cat)
	locals := extractLocalTemplateVars(src, len(src), vars, cat)
	scopedDotType := resolveScopedDotGoType(src, off, vars, locals, cat)

	// 2. Check multi-level struct field hover: .user.Name, .profile.User.Email, or $item.Name
	if holderChain, field, fStart, fEnd := nestedFieldSpanAtOffset(src, off); holderChain != "" && field != "" {
		if cat != nil {
			goType := strings.TrimPrefix(resolveExprChainGoType(holderChain, scopedDotType, vars, locals, cat), "[]")
			if goType != "" {
				if tbl := cat.Model(goType); tbl != nil {
					for _, c := range tbl.Columns {
						if c.GoName == field {
							return &HoverResult{
								Markdown: fmt.Sprintf("**`models.%s.%s`** (`%s`)\n\n- Table column: `%s.%s` (`%s`)\n- Defined in: `%s:%d`", goType, c.GoName, c.GoType, tbl.Name, c.Name, c.SQLType, filepath.Base(c.Loc.File), c.Loc.Line+1),
								Start:    fStart,
								End:      fEnd,
							}
						}
					}
					for _, r := range tbl.Relations {
						if r.Name == field {
							return &HoverResult{
								Markdown: fmt.Sprintf("**`models.%s.%s`** (relation → `models.%s`)\n\n- Defined in: `%s:%d`", goType, r.Name, r.TargetModel, filepath.Base(r.Loc.File), r.Loc.Line+1),
								Start:    fStart,
								End:      fEnd,
							}
						}
					}
				}
				if st := cat.Struct(goType); st != nil {
					for _, f := range st.Fields {
						if f.GoName == field {
							return &HoverResult{
								Markdown: fmt.Sprintf("**`%s.%s`** (`%s`)\n\n- Defined in: `%s:%d`", st.Name, f.GoName, f.GoType, filepath.Base(f.Loc.File), f.Loc.Line+1),
								Start:    fStart,
								End:      fEnd,
							}
						}
					}
				}
			}
		}
		return &HoverResult{
			Markdown: fmt.Sprintf("**Template Field `%s.%s`**\n\n- Base expression: `%s`\n- Field / key: `.%s`", holderChain, field, holderChain, field),
			Start:    fStart,
			End:      fEnd,
		}
	}

	// 3. Check local $var hover
	if lvar, lStart, lEnd := dollarVarSpanAtOffset(src, off); lvar != "" {
		for _, lv := range locals {
			if lv.Name == lvar {
				line, _ := offsetToLineCol(src, lv.Offset)
				typeSuffix := ""
				if lv.GoType != "" {
					typeSuffix = fmt.Sprintf(" (type `%s`)", lv.GoType)
				}
				return &HoverResult{
					Markdown: fmt.Sprintf("**Local Template Variable `%s`**%s\n\n- Assigned from: `%s` (line %d)", lv.Name, typeSuffix, lv.Expr, line+1),
					Start:    lStart,
					End:      lEnd,
				}
			}
		}
		return &HoverResult{
			Markdown: fmt.Sprintf("**Local Template Variable `%s`**", lvar),
			Start:    lStart,
			End:      lEnd,
		}
	}

	// 4. Context / shared variable or scoped field hover (.varName or $.varName)
	if dotWord, isRootDollar, dStart, dEnd := dotVarSpanAtOffset(src, off); dotWord != "" {
		return buildDotVariableHover(path, src, dotWord, isRootDollar, dStart, dEnd, scopedDotType, vars, cat)
	}

	// 4b. Bare dot (.) context hover (e.g., {{ template "layouts/header.tmpl" . }} or {{ . }})
	if dotPos, ok := bareDotAtOffset(src, off); ok {
		return buildBareDotHover(path, src, dotPos, scopedDotType, vars, cat)
	}

	// 5. Unprefixed identifier: custom template.FuncMap function, keyword, built-in function, or fallback variable check
	word, wStart, wEnd := templateVarSpanAtOffset(src, off)
	if word == "" {
		return nil
	}
	if cat != nil {
		if fn := cat.TemplateFuncs[word]; fn != nil {
			var b strings.Builder
			fmt.Fprintf(&b, "**Custom Template Function `%s`**\n\n- Signature: `%s`\n- Defined in: `%s:%d`", fn.Name, fn.Signature, filepath.Base(fn.Loc.File), fn.Loc.Line+1)
			if fn.Doc != "" {
				fmt.Fprintf(&b, "\n\n%s", fn.Doc)
			}
			return &HoverResult{Markdown: b.String(), Start: wStart, End: wEnd}
		}
	}

	// 6. Built-in keyword / function hover
	for _, kw := range tmplKeywords {
		if kw.Label == word {
			return &HoverResult{Markdown: fmt.Sprintf("**Go Template Keyword `%s`**\n\n%s", kw.Label, kw.Detail), Start: wStart, End: wEnd}
		}
	}
	for _, fn := range tmplBuiltins {
		if fn.Label == word {
			return &HoverResult{Markdown: fmt.Sprintf("**Go Template Function `%s`**\n\n%s", fn.Label, fn.Detail), Start: wStart, End: wEnd}
		}
	}

	// Fallback if a template variable is referenced without leading dot or matched in vars
	for _, vv := range vars {
		if vv.Key == word {
			return buildDotVariableHover(path, src, word, false, wStart, wEnd, scopedDotType, vars, cat)
		}
	}
	return nil
}

func buildDotVariableHover(path, src, word string, isRootDollar bool, start, end int, scopedDotType string, vars []indexer.ViewVar, cat *indexer.Catalog) *HoverResult {
	prefix := "."
	if isRootDollar {
		prefix = "$."
	}

	// 1. Scoped struct/model field when inside {{ range }} or {{ with }}
	if !isRootDollar && scopedDotType != "" && cat != nil {
		if ft, fLoc, ok := stepStructOrModelField(scopedDotType, word, cat); ok {
			var b strings.Builder
			fmt.Fprintf(&b, "**Scoped Field `.%s`** (`%s` on `%s`)\n\n", word, ft, scopedDotType)
			if tbl := cat.Model(strings.TrimPrefix(scopedDotType, "[]")); tbl != nil {
				for _, c := range tbl.Columns {
					if c.GoName == word {
						fmt.Fprintf(&b, "- Table column: `%s.%s` (`%s`)\n", tbl.Name, c.Name, c.SQLType)
						break
					}
				}
			}
			if fLoc.Valid() {
				fmt.Fprintf(&b, "- Defined in: `%s:%d`\n", filepath.Base(fLoc.File), fLoc.Line+1)
			}
			return &HoverResult{Markdown: strings.TrimSpace(b.String()), Start: start, End: end}
		}
	}

	// 2. Built-in Goravel .csrf_token
	if word == "csrf_token" {
		return &HoverResult{
			Markdown: "**Template Variable `.csrf_token`** (`string`)\n\n- Goravel CSRF token automatically injected by `VerifyCsrfToken` middleware",
			Start:    start,
			End:      end,
		}
	}

	var passedLines []string
	var matchedValueExprs []string
	inferredType := ""
	for _, vv := range vars {
		if vv.Key == word {
			if inferredType == "" && vv.GoType != "" {
				inferredType = vv.GoType
			}
			if vv.ValueExpr != "" {
				matchedValueExprs = append(matchedValueExprs, vv.ValueExpr)
			}
			srcInfo := fmt.Sprintf("`%s`", vv.ValueExpr)
			if vv.GoType != "" {
				srcInfo += fmt.Sprintf(" (type `%s`)", vv.GoType)
			}
			if vv.Shared {
				srcInfo += fmt.Sprintf(" shared via `View().Share` in `%s` (`%s:%d`)", vv.Controller, filepath.Base(vv.Loc.File), vv.Loc.Line+1)
			} else if vv.Inherited != "" {
				srcInfo += fmt.Sprintf(" via parent `%s` from `%s` (`%s:%d`)", vv.Inherited, vv.Controller, filepath.Base(vv.Loc.File), vv.Loc.Line+1)
			} else if vv.Controller != "" {
				srcInfo += fmt.Sprintf(" in `%s` (`%s:%d`)", vv.Controller, filepath.Base(vv.Loc.File), vv.Loc.Line+1)
			}
			passedLines = append(passedLines, "- Passed as "+srcInfo)
		}
	}

	vi := (*indexer.ViewItem)(nil)
	if cat != nil {
		vi = cat.ViewForPath(path)
	}
	cfgItem := matchConfigForTemplateVar(word, matchedValueExprs, cat)
	if inferredType == "" && cfgItem != nil {
		inferredType = "string"
	}
	modelName, modelCol := matchModelFieldForTemplateVar(word, vi, cat)
	if inferredType == "" && modelCol != nil && modelCol.GoType != "" {
		inferredType = modelCol.GoType
	}

	var b strings.Builder
	typeBadge := ""
	if inferredType != "" {
		typeBadge = fmt.Sprintf(" (`%s`)", inferredType)
	}
	rootBadge := ""
	if isRootDollar {
		rootBadge = " *(root context)*"
	}
	fmt.Fprintf(&b, "**Template Variable `%s%s`**%s%s\n\n", prefix, word, typeBadge, rootBadge)

	if len(passedLines) > 0 {
		b.WriteString(strings.Join(passedLines, "\n") + "\n")
	} else {
		// Variable is not directly in vars for this template — provide rich contextual intelligence
		viewDisplay := filepath.Base(path)
		if vi != nil && vi.Name != "" {
			viewDisplay = vi.Name
		}
		if vi != nil && len(vi.Callers) == 0 && len(vi.IncludedBy) == 0 {
			fmt.Fprintf(&b, "- **View**: `%s` *(unreferenced view — 0 callers in controllers or routes)*\n", viewDisplay)
		} else {
			fmt.Fprintf(&b, "- **Status**: Not passed by callers of `%s`\n", viewDisplay)
			var candNames []string
			for _, vv := range vars {
				candNames = append(candNames, vv.Key)
			}
			if best := closestVarMatch(word, candNames); best != "" {
				fmt.Fprintf(&b, "- **Did you mean**: `.%s` *(passed to this view)*\n", best)
			}
		}

		// Check if any included partial (e.g. layouts/header.tmpl) uses this variable or a related variable
		if vi != nil && cat != nil {
			for _, inc := range vi.Includes {
				child := cat.LookupView(inc.Target)
				if child == nil {
					continue
				}
				for _, uv := range child.UsedVars {
					if uv == word || isRelatedVarName(word, uv) {
						fmt.Fprintf(&b, "- **Included Partial**: `%s` uses `.%s`\n", child.Name, uv)
					}
				}
			}
		}

		// Search across other views in the workspace for exact or related ViewVar definitions
		if cat != nil {
			crossKey, crossVars := findCrossViewVars(word, cat)
			if len(crossVars) > 0 {
				crossType := ""
				for _, cv := range crossVars {
					if cv.GoType != "" {
						crossType = " · `" + cv.GoType + "`"
						break
					}
				}
				if crossKey == word {
					fmt.Fprintf(&b, "- **Passed in Other Views** (`.%s`%s):\n", crossKey, crossType)
				} else {
					fmt.Fprintf(&b, "- **Related Variable Across Views** (`.%s`%s):\n", crossKey, crossType)
				}
				limit := 4
				if len(crossVars) < limit {
					limit = len(crossVars)
				}
				for _, cv := range crossVars[:limit] {
					if cv.ValueExpr != "" {
						matchedValueExprs = append(matchedValueExprs, cv.ValueExpr)
					}
					fmt.Fprintf(&b, "  - `%s` in `%s` (`%s:%d`)\n", cv.ValueExpr, cv.Controller, filepath.Base(cv.Loc.File), cv.Loc.Line+1)
				}
				if cfgItem == nil {
					cfgItem = matchConfigForTemplateVar(word, matchedValueExprs, cat)
				}
			}
		}
	}

	// Append linked Goravel Config entry if matched
	if cfgItem != nil {
		locInfo := ""
		if cfgItem.Loc.Valid() {
			locInfo = fmt.Sprintf(" (`%s:%d`)", filepath.Base(cfgItem.Loc.File), cfgItem.Loc.Line+1)
		}
		fmt.Fprintf(&b, "- **Config Key**: `%s`%s\n", cfgItem.Key, locInfo)
		var meta []string
		if cfgItem.EnvKey != "" {
			meta = append(meta, fmt.Sprintf("Env: `%s`", cfgItem.EnvKey))
		}
		if cfgItem.Default != "" {
			meta = append(meta, fmt.Sprintf("Default: `%s`", cfgItem.Default))
		}
		if cfgItem.EnvVal != "" {
			meta = append(meta, fmt.Sprintf("Value: `%s`", cfgItem.EnvVal))
		}
		if len(meta) > 0 {
			fmt.Fprintf(&b, "  - %s\n", strings.Join(meta, " · "))
		}
	}

	// Append linked Model field if matched (e.g. .meetingId -> models.Meeting.ID)
	if modelCol != nil {
		locInfo := ""
		if modelCol.Loc.Valid() {
			locInfo = fmt.Sprintf(" (`%s:%d`)", filepath.Base(modelCol.Loc.File), modelCol.Loc.Line+1)
		}
		fmt.Fprintf(&b, "- **Related Model Field**: `models.%s.%s` (`%s`, column `%s`)%s\n", modelName, modelCol.GoName, modelCol.GoType, modelCol.Name, locInfo)
	}

	// If inferredType is a Model or Struct, list its fields
	if inferredType != "" && cat != nil {
		baseType := strings.TrimPrefix(inferredType, "[]")
		if fItems := structFieldCompletions(baseType, cat); len(fItems) > 0 {
			var fNames []string
			limit := 8
			if len(fItems) < limit {
				limit = len(fItems)
			}
			for _, fi := range fItems[:limit] {
				fNames = append(fNames, fmt.Sprintf("`.%s.%s`", word, fi.Label))
			}
			if len(fItems) > limit {
				fNames = append(fNames, "...")
			}
			fmt.Fprintf(&b, "- **Available Fields**: %s\n", strings.Join(fNames, ", "))
		}
	}

	// Show occurrences in current template
	if spans := findTemplateVarSpansInContent(src, word); len(spans) > 0 {
		var lineNums []string
		seenLine := map[int]bool{}
		for _, sp := range spans {
			ln, _ := offsetToLineCol(src, sp[0])
			if !seenLine[ln+1] {
				seenLine[ln+1] = true
				lineNums = append(lineNums, strconv.Itoa(ln+1))
			}
		}
		fmt.Fprintf(&b, "- **Occurrences in template**: %d (line %s)\n", len(spans), strings.Join(lineNums, ", "))
	}

	return &HoverResult{Markdown: strings.TrimSpace(b.String()), Start: start, End: end}
}

func buildBareDotHover(path, src string, dotPos int, scopedDotType string, vars []indexer.ViewVar, cat *indexer.Catalog) *HoverResult {
	var b strings.Builder
	if scopedDotType != "" {
		fmt.Fprintf(&b, "**Scoped Template Context `.`** (`%s`)\n\n", scopedDotType)
		if cat != nil {
			if fItems := structFieldCompletions(strings.TrimPrefix(scopedDotType, "[]"), cat); len(fItems) > 0 {
				var fNames []string
				for _, fi := range fItems {
					fNames = append(fNames, "`."+fi.Label+"`")
				}
				fmt.Fprintf(&b, "- Fields: %s\n", strings.Join(fNames, ", "))
			}
		}
		return &HoverResult{Markdown: strings.TrimSpace(b.String()), Start: dotPos, End: dotPos + 1}
	}

	_, targetName, _, _ := templateDirectiveSpanAtOffset(src, dotPos)
	if targetName != "" && cat != nil {
		fmt.Fprintf(&b, "**Template Context `.` passed to `%s`**\n\n", targetName)
		if child := cat.LookupView(targetName); child != nil && len(child.UsedVars) > 0 {
			var uvars []string
			for _, uv := range child.UsedVars {
				uvars = append(uvars, "`."+uv+"`")
			}
			fmt.Fprintf(&b, "- Expected by `%s`: %s\n", child.Name, strings.Join(uvars, ", "))
		}
	} else {
		fmt.Fprintf(&b, "**Root Template Context `.`**\n\n")
	}

	if len(vars) > 0 {
		seen := map[string]bool{}
		var vList []string
		for _, vv := range vars {
			if !seen[vv.Key] {
				seen[vv.Key] = true
				vList = append(vList, "`."+vv.Key+"`")
			}
		}
		fmt.Fprintf(&b, "- Passed variables: %s\n", strings.Join(vList, ", "))
	} else if cat != nil {
		if vi := cat.ViewForPath(path); vi != nil && len(vi.DirectVars) > 0 {
			var dList []string
			for _, dv := range vi.DirectVars {
				dList = append(dList, "`."+dv+"`")
			}
			fmt.Fprintf(&b, "- Referenced in `%s`: %s\n", vi.Name, strings.Join(dList, ", "))
		}
	}
	return &HoverResult{Markdown: strings.TrimSpace(b.String()), Start: dotPos, End: dotPos + 1}
}

var configCallKeyRe = regexp.MustCompile(`Config\(\)\.[A-Za-z]+\("([^"]+)"`)

func matchConfigForTemplateVar(word string, valueExprs []string, cat *indexer.Catalog) *indexer.ConfigItem {
	if cat == nil || len(cat.Configs) == 0 {
		return nil
	}
	for _, ve := range valueExprs {
		if m := configCallKeyRe.FindStringSubmatch(ve); m != nil {
			if ci := cat.Configs[m[1]]; ci != nil {
				return ci
			}
		}
	}
	snake := camelToSnake(word)
	dotCandidate := strings.Replace(snake, "_", ".", 1)
	if ci := cat.Configs[dotCandidate]; ci != nil {
		return ci
	}
	if snake == "supabase_key" {
		if ci := cat.Configs["supabase.anon_key"]; ci != nil {
			return ci
		}
	}
	flatWord := strings.ToLower(strings.ReplaceAll(word, "_", ""))
	var keys []string
	for k := range cat.Configs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		normK := strings.ToLower(strings.ReplaceAll(k, ".", "_"))
		if normK == snake || strings.ReplaceAll(normK, "_", "") == flatWord {
			return cat.Configs[k]
		}
	}
	return nil
}

func matchModelFieldForTemplateVar(word string, vi *indexer.ViewItem, cat *indexer.Catalog) (string, *indexer.Column) {
	if cat == nil || len(cat.Models) == 0 {
		return "", nil
	}
	snake := camelToSnake(word)
	if strings.HasSuffix(snake, "_id") {
		prefix := strings.TrimSuffix(snake, "_id")
		for mName, tbl := range cat.Models {
			if strings.EqualFold(mName, prefix) || strings.EqualFold(tbl.Name, prefix+"s") || strings.EqualFold(tbl.Name, prefix) {
				for _, c := range tbl.Columns {
					if strings.EqualFold(c.Name, "id") || strings.EqualFold(c.Name, snake) {
						return mName, c
					}
				}
			}
		}
	}
	if vi != nil {
		parts := strings.Split(vi.Name, "/")
		if len(parts) > 1 {
			dirPrefix := strings.TrimSuffix(parts[0], "s")
			for mName, tbl := range cat.Models {
				if strings.EqualFold(mName, dirPrefix) || strings.EqualFold(tbl.Name, parts[0]) {
					for _, c := range tbl.Columns {
						if strings.EqualFold(c.Name, snake) || strings.EqualFold(c.GoName, word) {
							return mName, c
						}
					}
				}
			}
		}
	}
	return "", nil
}

func findCrossViewVars(word string, cat *indexer.Catalog) (string, []indexer.ViewVar) {
	if cat == nil || len(cat.ViewVars) == 0 {
		return "", nil
	}
	var viewKeys []string
	for k := range cat.ViewVars {
		viewKeys = append(viewKeys, k)
	}
	sort.Strings(viewKeys)

	// 1. Exact key match across views
	seen := map[string]bool{}
	var exactOut []indexer.ViewVar
	for _, vk := range viewKeys {
		for _, vv := range cat.ViewVars[vk] {
			if vv.Inherited != "" || vv.Key != word {
				continue
			}
			sig := vv.Key + "|" + vv.Controller + "|" + vv.Loc.File + ":" + strconv.Itoa(vv.Loc.Line)
			if !seen[sig] {
				seen[sig] = true
				exactOut = append(exactOut, vv)
			}
		}
	}
	if len(exactOut) > 0 {
		return word, exactOut
	}

	// 2. Related / snake_case / fuzzy match across views
	matchedKey := ""
	var relOut []indexer.ViewVar
	for _, vk := range viewKeys {
		for _, vv := range cat.ViewVars[vk] {
			if vv.Inherited != "" {
				continue
			}
			if isRelatedVarName(word, vv.Key) {
				if matchedKey == "" {
					matchedKey = vv.Key
				}
				if vv.Key != matchedKey {
					continue
				}
				sig := vv.Key + "|" + vv.Controller + "|" + vv.Loc.File + ":" + strconv.Itoa(vv.Loc.Line)
				if !seen[sig] {
					seen[sig] = true
					relOut = append(relOut, vv)
				}
			}
		}
	}
	return matchedKey, relOut
}

func isRelatedVarName(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	sa := camelToSnake(a)
	sb := camelToSnake(b)
	if sa == sb || strings.ReplaceAll(sa, "_", "") == strings.ReplaceAll(sb, "_", "") {
		return true
	}
	ta := strings.Split(sa, "_")
	tb := strings.Split(sb, "_")
	if len(ta) >= 2 && len(tb) >= 2 && ta[0] == tb[0] && ta[len(ta)-1] == tb[len(tb)-1] {
		return true
	}
	return levenshtein(sa, sb) <= 2
}

func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && s[i-1] != '_' && !(s[i-1] >= 'A' && s[i-1] <= 'Z') {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

// TemplateDefinition jumps from .varName, .a.b.c, $local, custom FuncMap function, {{ template "name" }}, or HTML route/asset in a template file to its definition.
func TemplateDefinition(path, src string, cat *indexer.Catalog, off int) []indexer.Location {
	if !IsTemplateFile(path) {
		return nil
	}
	if !isInsideTemplateAction(src, off) {
		return htmlRouteAndAssetDefinition(src, off, cat)
	}

	// 1. Jump from {{ template "name" }} or {{ block "name" }} to target template file (or from {{ define "name" }} to Go callers)
	if directive, targetName, _, nEnd := templateDirectiveSpanAtOffset(src, off); directive != "" && targetName != "" && off <= nEnd+1 && cat != nil {
		var locs []indexer.Location
		if directive == "define" {
			if vi := cat.LookupView(targetName); vi != nil {
				for _, c := range vi.Callers {
					if c.Loc.Valid() {
						locs = append(locs, c.Loc)
					}
				}
			}
			return locs
		}
		if vi := cat.LookupView(targetName); vi != nil {
			for _, d := range vi.Defines {
				if (d.Name == targetName || strings.TrimSuffix(d.Name, filepath.Ext(d.Name)) == strings.TrimSuffix(targetName, filepath.Ext(targetName))) && d.Loc.Valid() {
					locs = append(locs, d.Loc)
				}
			}
			if len(locs) == 0 {
				if vi.DefineLoc.Valid() {
					locs = append(locs, vi.DefineLoc)
				} else if vi.Loc.Valid() {
					locs = append(locs, vi.Loc)
				}
			}
			for _, pl := range vi.PackageLocs {
				if pl.Valid() {
					locs = append(locs, pl)
				}
			}
			return locs
		}
	}

	vars := resolveViewVarsForTemplate(path, cat)
	locals := extractLocalTemplateVars(src, len(src), vars, cat)
	scopedDotType := resolveScopedDotGoType(src, off, vars, locals, cat)

	// 2. Jump from .user.Name or .profile.User.Email to Go struct field definition
	if holderChain, field := nestedFieldAtOffset(src, off); holderChain != "" && field != "" && cat != nil {
		goType := strings.TrimPrefix(resolveExprChainGoType(holderChain, scopedDotType, vars, locals, cat), "[]")
		if goType != "" {
			if _, fLoc, ok := stepStructOrModelField(goType, field, cat); ok && fLoc.Valid() {
				return []indexer.Location{fLoc}
			}
		}
	}

	// 3. Jump from $localVar to its := assignment in the current template
	if lvar := dollarVarAtOffset(src, off); lvar != "" {
		for _, lv := range locals {
			if lv.Name == lvar {
				line, col := offsetToLineCol(src, lv.Offset)
				return []indexer.Location{{File: path, Line: line, Col: col}}
			}
		}
	}

	// 4. Jump from .varName (or scoped field, config entry, related cross-view var, model field) or custom FuncMap function
	word, _, _, _ := dotVarSpanAtOffset(src, off)
	if word == "" {
		word = templateVarAtOffset(src, off)
	}
	if word == "" {
		return nil
	}
	if scopedDotType != "" && cat != nil {
		if _, fLoc, ok := stepStructOrModelField(scopedDotType, word, cat); ok && fLoc.Valid() {
			return []indexer.Location{fLoc}
		}
	}
	var locs []indexer.Location
	var matchedValueExprs []string
	for _, vv := range vars {
		if vv.Key == word {
			if vv.ValueExpr != "" {
				matchedValueExprs = append(matchedValueExprs, vv.ValueExpr)
			}
			if vv.Loc.Valid() {
				locs = append(locs, vv.Loc)
			}
		}
	}
	if len(locs) > 0 {
		return locs
	}
	if cat != nil {
		if fn := cat.TemplateFuncs[word]; fn != nil && fn.Loc.Valid() {
			return []indexer.Location{fn.Loc}
		}
		if _, crossVars := findCrossViewVars(word, cat); len(crossVars) > 0 {
			for _, cv := range crossVars {
				if cv.ValueExpr != "" {
					matchedValueExprs = append(matchedValueExprs, cv.ValueExpr)
				}
				if cv.Loc.Valid() {
					locs = append(locs, cv.Loc)
				}
			}
		}
		if cfgItem := matchConfigForTemplateVar(word, matchedValueExprs, cat); cfgItem != nil && cfgItem.Loc.Valid() {
			locs = append(locs, cfgItem.Loc)
		}
		if _, col := matchModelFieldForTemplateVar(word, cat.ViewForPath(path), cat); col != nil && col.Loc.Valid() {
			locs = append(locs, col.Loc)
		}
	}
	return locs
}

var (
	blockOpenKeywordRe = regexp.MustCompile(`^\s*(define|if|range|with|block)\b`)
	blockEndKeywordRe  = regexp.MustCompile(`^\s*end\b`)
)

// TemplateDiagnostics validates template syntax (empty file, unclosed {{, unbalanced block/end, missing included templates, mismatched define names, unknown/under-applied custom functions, unpassed variables with fuzzy match, and broken public/ static assets).
func TemplateDiagnostics(path, src string, cat *indexer.Catalog) []Diag {
	if !IsTemplateFile(path) {
		return nil
	}
	var diags []Diag

	// 0. Check for empty template file (e.g. resources/views/layouts/auth.tmpl)
	if strings.TrimSpace(src) == "" {
		return []Diag{
			{
				Start:    0,
				End:      len(src),
				Severity: 4, // Hint
				Message:  "Template file is empty — use Quick Fix to scaffold a '{{ define }}' template block",
			},
		}
	}

	// 1. Check for unclosed {{ action
	searchFrom := 0
	for searchFrom < len(src) {
		openIdx := strings.Index(src[searchFrom:], "{{")
		if openIdx < 0 {
			break
		}
		absOpen := searchFrom + openIdx
		closeIdx := strings.Index(src[absOpen+2:], "}}")
		if closeIdx < 0 {
			diags = append(diags, Diag{
				Start:    absOpen,
				End:      len(src),
				Severity: 1, // Error
				Message:  "Unclosed Go template action '{{'",
			})
			break
		}
		searchFrom = absOpen + 2 + closeIdx + 2
	}

	// 2. Check block balance (define/if/range/with/block vs end) and included templates
	type openBlock struct {
		kw          string
		start       int
		end         int
		rebindsDot  bool
	}
	var stack []openBlock
	localDefines := map[string]bool{}
	var defineSpans []struct {
		name  string
		start int
		end   int
	}
	var includeSpans []struct {
		target string
		start  int
		end    int
	}

	vars := resolveViewVarsForTemplate(path, cat)
	knownVars := map[string]bool{"csrf_token": true}
	var knownVarList []string
	for _, vv := range vars {
		if !knownVars[vv.Key] {
			knownVars[vv.Key] = true
			knownVarList = append(knownVarList, vv.Key)
		}
	}
	vi := (*indexer.ViewItem)(nil)
	if cat != nil {
		vi = cat.ViewForPath(path)
	}
	hasKnownCallers := len(vars) > 0 && vi != nil && (len(vi.Callers) > 0 || len(vi.IncludedBy) > 0)
	reportedVars := map[string]bool{}

	matches := tmplActionRe.FindAllStringSubmatchIndex(src, -1)
	for _, m := range matches {
		actStart, actEnd := m[0], m[1]
		bodyStart, bodyEnd := m[2], m[3]
		body := src[bodyStart:bodyEnd]
		trimmed := strings.TrimSpace(body)
		if strings.HasPrefix(trimmed, "/*") {
			continue
		}
		if dm := tmplDefineLeadRe.FindStringSubmatchIndex(body); dm != nil {
			nStart, nEnd := bodyStart+dm[2], bodyStart+dm[3]
			dName := src[nStart:nEnd]
			localDefines[dName] = true
			defineSpans = append(defineSpans, struct {
				name  string
				start int
				end   int
			}{dName, nStart, nEnd})
		}
		if im := tmplIncludeLeadRe.FindStringSubmatchIndex(body); im != nil {
			nStart, nEnd := bodyStart+im[2], bodyStart+im[3]
			tName := src[nStart:nEnd]
			includeSpans = append(includeSpans, struct {
				target string
				start  int
				end    int
			}{tName, nStart, nEnd})
		}
		if bm := blockOpenKeywordRe.FindStringSubmatch(body); bm != nil {
			kw := bm[1]
			rebinds := kw == "range" || kw == "with"
			stack = append(stack, openBlock{kw: kw, start: actStart, end: actEnd, rebindsDot: rebinds})
		} else if blockEndKeywordRe.MatchString(body) {
			if len(stack) == 0 {
				diags = append(diags, Diag{
					Start:    actStart,
					End:      actEnd,
					Severity: 1,
					Message:  "Unexpected '{{ end }}' without matching '{{ if / range / with / define / block }}'",
				})
			} else {
				stack = stack[:len(stack)-1]
			}
		}

		// Check custom template function calls & pipe targets
		if cat != nil && len(cat.TemplateFuncs) > 0 {
			diags = append(diags, checkTemplateActionFuncs(body, bodyStart, cat)...)
		}

		// Check unpassed top-level .var references
		if cat != nil {
			dotRebound := false
			for _, sb := range stack {
				if sb.rebindsDot {
					dotRebound = true
					break
				}
			}
			isConditionalAction := strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "with ") || strings.Contains(trimmed, "default ")
			if !isConditionalAction {
				dotVarMatches := tmplDotVarRe.FindAllStringSubmatchIndex(body, -1)
				for _, vm := range dotVarMatches {
					hasDollar := vm[2] >= 0 && vm[3] > vm[2]
					if dotRebound && !hasDollar {
						continue
					}
					vStart, vEnd := bodyStart+vm[4], bodyStart+vm[5]
					vName := src[vStart:vEnd]
					if !knownVars[vName] && !reportedVars[vName] {
						reportedVars[vName] = true
						viewName := filepath.Base(path)
						if vi != nil && vi.Name != "" {
							viewName = vi.Name
						}
						var msg string
						if hasKnownCallers {
							msg = fmt.Sprintf("Template variable '.%s' is not passed by callers of '%s'", vName, viewName)
						} else {
							msg = fmt.Sprintf("Unknown template variable '.%s' in '%s' (no callers pass this variable)", vName, viewName)
						}
						if best := closestVarMatch(vName, knownVarList); best != "" {
							msg = fmt.Sprintf("Template variable '.%s' is not passed to '%s' (did you mean '.%s'?)", vName, viewName, best)
						}
						diags = append(diags, Diag{
							Start:    vStart - 1,
							End:      vEnd,
							Severity: 1, // Error
							Message:  msg,
						})
					}
				}
			}
		}
	}
	for _, unclosed := range stack {
		diags = append(diags, Diag{
			Start:    unclosed.start,
			End:      unclosed.end,
			Severity: 1,
			Message:  fmt.Sprintf("Unclosed '{{ %s }}' block (missing '{{ end }}')", unclosed.kw),
		})
	}

	if cat != nil && (len(cat.Views) > 0 || len(cat.ViewItems) > 0) {
		for _, inc := range includeSpans {
			if localDefines[inc.target] {
				continue
			}
			if cat.LookupView(inc.target) == nil {
				if _, ok := cat.Views[inc.target]; !ok {
					diags = append(diags, Diag{
						Start:    inc.start,
						End:      inc.end,
						Severity: 2, // Warning
						Message:  fmt.Sprintf("Included template '%s' not found in resources/views/ or registered package views", inc.target),
					})
				}
			}
		}
		if len(defineSpans) == 1 {
			if vi != nil && vi.Source == "app" {
				d := defineSpans[0]
				noExt := strings.TrimSuffix(vi.Name, filepath.Ext(vi.Name))
				if d.name != vi.Name && d.name != noExt {
					diags = append(diags, Diag{
						Start:    d.start,
						End:      d.end,
						Severity: 4, // Hint
						Message:  fmt.Sprintf("Template define name '%s' does not match relative view path '%s'", d.name, vi.Name),
					})
				}
			}
		}
	}

	// 3. Check static asset references in public/ if public/ directory exists
	if cat != nil && cat.Root != "" {
		diags = append(diags, checkStaticAssetLinks(src, cat)...)
	}
	return diags
}

func checkTemplateActionFuncs(body string, bodyStart int, cat *indexer.Catalog) []Diag {
	var diags []Diag
	isKnownKwOrBuiltin := func(name string) bool {
		for _, kw := range tmplKeywords {
			if kw.Label == name {
				return true
			}
		}
		for _, fn := range tmplBuiltins {
			if fn.Label == name {
				return true
			}
		}
		return name == "else" || name == "true" || name == "false"
	}

	// Check pipeline segments: seg0 | seg1 | seg2
	segments := strings.Split(body, "|")
	offsetInBody := 0
	for segIdx, seg := range segments {
		if idx := strings.Index(body[offsetInBody:], seg); idx >= 0 {
			offsetInBody += idx
		}
		trimSeg := strings.TrimSpace(seg)
		if segIdx == 0 {
			if idx := strings.Index(trimSeg, ":="); idx >= 0 {
				trimSeg = strings.TrimSpace(trimSeg[idx+2:])
			}
			for _, prefixKw := range []string{"if ", "with ", "range ", "else if "} {
				if strings.HasPrefix(trimSeg, prefixKw) {
					trimSeg = strings.TrimSpace(trimSeg[len(prefixKw):])
				}
			}
		}
		fields := strings.Fields(trimSeg)
		if len(fields) > 0 {
			head := fields[0]
			isIdent := tmplIdentRe.MatchString(head)
			if isIdent && (segIdx > 0 || len(fields) > 1) && !isKnownKwOrBuiltin(head) {
				relIdx := strings.Index(seg, head)
				fnStart := bodyStart + offsetInBody + relIdx
				fnEnd := fnStart + len(head)
				fnItem := cat.TemplateFuncs[head]
				if fnItem == nil {
					diags = append(diags, Diag{
						Start:    fnStart,
						End:      fnEnd,
						Severity: 2, // Warning
						Message:  fmt.Sprintf("Unknown template function '%s' (not in Go built-ins or registered FuncMap)", head),
					})
				} else {
					passedArgs := countTemplateCallArgs(trimSeg[len(head):])
					if segIdx > 0 {
						passedArgs++ // piped value counts as last arg
					}
					if passedArgs < fnItem.MinArgs {
						diags = append(diags, Diag{
							Start:    fnStart,
							End:      fnEnd,
							Severity: 2, // Warning
							Message:  fmt.Sprintf("Template function '%s' expects at least %d argument(s) (%s), got %d", head, fnItem.MinArgs, fnItem.Signature, passedArgs),
						})
					}
				}
			}
		}
		offsetInBody += len(seg)
	}
	return diags
}

func countTemplateCallArgs(argText string) int {
	s := strings.TrimSpace(argText)
	if s == "" {
		return 0
	}
	count := 0
	inStr := byte(0)
	parenDepth := 0
	inToken := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr != 0 {
			if ch == inStr && (i == 0 || s[i-1] != '\\') {
				inStr = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inStr = ch
			if parenDepth == 0 && !inToken {
				inToken = true
				count++
			}
			continue
		}
		if ch == '(' {
			if parenDepth == 0 && !inToken {
				inToken = true
				count++
			}
			parenDepth++
			continue
		}
		if ch == ')' {
			if parenDepth > 0 {
				parenDepth--
			}
			if parenDepth == 0 {
				inToken = false
			}
			continue
		}
		if parenDepth > 0 {
			continue
		}
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			inToken = false
		} else if !inToken {
			inToken = true
			count++
		}
	}
	return count
}

func closestVarMatch(target string, candidates []string) string {
	best := ""
	bestDist := 99
	normTarget := strings.ToLower(strings.ReplaceAll(target, "_", ""))
	for _, c := range candidates {
		normCand := strings.ToLower(strings.ReplaceAll(c, "_", ""))
		if normCand == normTarget {
			return c
		}
		d := levenshtein(strings.ToLower(target), strings.ToLower(c))
		if d <= 3 && d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			ins := cur[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			m := ins
			if del < m {
				m = del
			}
			if sub < m {
				m = sub
			}
			cur[j] = m
		}
		prev = cur
	}
	return prev[len(b)]
}

// TemplateCodeActions provides Quick Fixes inside .tmpl files (scaffold empty file, sync define name, create missing partial, fix typo var or add var to controller map).
func TemplateCodeActions(path, src string, cat *indexer.Catalog, off int) []CodeAction {
	if !IsTemplateFile(path) || cat == nil {
		return nil
	}
	var actions []CodeAction

	// 1. Empty template file Quick Fix
	if strings.TrimSpace(src) == "" {
		defName := filepath.Base(path)
		if vi := cat.ViewForPath(path); vi != nil && vi.Name != "" {
			defName = vi.Name
		}
		scaffold := fmt.Sprintf("{{ define %q }}\n<div>\n  \n</div>\n{{ end }}\n", defName)
		actions = append(actions, CodeAction{
			Title: fmt.Sprintf("Scaffold '{{ define %q }}' template block", defName),
			Kind:  "quickfix",
			Edits: []TextEdit{
				{
					File:    path,
					Start:   0,
					End:     len(src),
					NewText: scaffold,
				},
			},
		})
		return actions
	}

	directive, targetName, nStart, nEnd := templateDirectiveSpanAtOffset(src, off)
	if directive == "define" && targetName != "" {
		if vi := cat.ViewForPath(path); vi != nil && vi.Name != targetName {
			actions = append(actions, CodeAction{
				Title: fmt.Sprintf("Sync {{ define }} name to '%s'", vi.Name),
				Kind:  "quickfix",
				Edits: []TextEdit{
					{
						File:    path,
						Start:   nStart,
						End:     nEnd,
						NewText: vi.Name,
					},
				},
			})
		}
	} else if (directive == "template" || directive == "block") && targetName != "" && cat.Root != "" {
		if cat.LookupView(targetName) == nil {
			relName := targetName
			if filepath.Ext(relName) == "" {
				relName += ".tmpl"
			}
			targetFile := filepath.Join(cat.Root, "resources", "views", filepath.FromSlash(relName))
			content := fmt.Sprintf("{{ define %q }}\n\n{{ end }}\n", targetName)
			actions = append(actions, CodeAction{
				Title: fmt.Sprintf("Create partial template 'resources/views/%s'", relName),
				Kind:  "quickfix",
				Edits: []TextEdit{
					{
						File:    targetFile,
						Start:   0,
						End:     0,
						NewText: content,
					},
				},
			})
		}
	}

	// Quick Fixes on unpassed template variable `.varName`
	if isInsideTemplateAction(src, off) {
		word, wStart, wEnd := templateVarSpanAtOffset(src, off)
		if word != "" {
			vars := resolveViewVarsForTemplate(path, cat)
			known := map[string]bool{"csrf_token": true}
			var candList []string
			for _, vv := range vars {
				if !known[vv.Key] {
					known[vv.Key] = true
					candList = append(candList, vv.Key)
				}
			}
			if !known[word] {
				if best := closestVarMatch(word, candList); best != "" {
					actions = append(actions, CodeAction{
						Title: fmt.Sprintf("Change '.%s' to '.%s'", word, best),
						Kind:  "quickfix",
						Edits: []TextEdit{
							{
								File:    path,
								Start:   wStart,
								End:     wEnd,
								NewText: best,
							},
						},
					})
				}
				if vi := cat.ViewForPath(path); vi != nil {
					for _, caller := range vi.Callers {
						if caller.Method == "Make" && caller.Loc.Valid() {
							if b, err := os.ReadFile(caller.Loc.File); err == nil {
								cSrc := string(b)
								calls, fset := findViewCalls(caller.Loc.File, cSrc)
								for _, vc := range calls {
									if vc.DataMap != nil && fset != nil && len(vc.Names) > 0 && sameViewName(vc.Names[0].Name, vi.Name, cat) {
										lbrace := fset.Position(vc.DataMap.Lbrace).Offset
										actions = append(actions, CodeAction{
											Title: fmt.Sprintf("Add %q to View().Make data map in %s", word, filepath.Base(caller.Loc.File)),
											Kind:  "quickfix",
											Edits: []TextEdit{
												{
													File:    caller.Loc.File,
													Start:   lbrace + 1,
													End:     lbrace + 1,
													NewText: fmt.Sprintf("\n\t\t%q: \"\",", word),
												},
											},
										})
										break
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return actions
}

// TemplateLens describes a CodeLens item for a template file.
type TemplateLens struct {
	Line  int
	Col   int
	Title string
	File  string
	TLine int
	TCol  int
}

// TemplateCodeLenses returns CodeLens items for a template file (Go callers, parent template includes, package overrides, and orphaned status).
func TemplateCodeLenses(path string, cat *indexer.Catalog) []TemplateLens {
	if !IsTemplateFile(path) || cat == nil {
		return nil
	}
	vi := cat.ViewForPath(path)
	if vi == nil {
		return nil
	}
	line := 0
	if vi.DefineLoc.Valid() && filepath.Clean(vi.DefineLoc.File) == filepath.Clean(path) {
		line = vi.DefineLoc.Line
	}
	var lenses []TemplateLens
	seenCaller := map[string]bool{}
	for _, c := range vi.Callers {
		if !c.Loc.Valid() {
			continue
		}
		key := fmt.Sprintf("%s:%d:%s", c.Loc.File, c.Loc.Line, c.Method)
		if seenCaller[key] {
			continue
		}
		seenCaller[key] = true
		title := fmt.Sprintf("$(symbol-method) View().%s in %s (%s)", c.Method, c.Controller, filepath.Base(c.Loc.File))
		if c.Route != "" {
			title = fmt.Sprintf("$(globe) %s → %s (%s)", c.Route, c.Controller, filepath.Base(c.Loc.File))
		}
		lenses = append(lenses, TemplateLens{
			Line:  line,
			Col:   0,
			Title: title,
			File:  c.Loc.File,
			TLine: c.Loc.Line,
			TCol:  c.Loc.Col,
		})
	}

	// Check parent templates that include this template via {{ template "..." }}
	includedCount := 0
	for _, other := range cat.ViewItems {
		for _, inc := range other.Includes {
			if target := cat.LookupView(inc.Target); target != nil && target.Name == vi.Name && inc.Loc.Valid() {
				includedCount++
				lenses = append(lenses, TemplateLens{
					Line:  line,
					Col:   0,
					Title: fmt.Sprintf("$(references) Included by %s", other.Name),
					File:  inc.Loc.File,
					TLine: inc.Loc.Line,
					TCol:  inc.Loc.Col,
				})
			}
		}
	}

	// Check package override relationship
	cleanPath := filepath.Clean(path)
	if filepath.Clean(vi.Loc.File) == cleanPath && len(vi.PackageLocs) > 0 {
		pl := vi.PackageLocs[0]
		lenses = append(lenses, TemplateLens{
			Line:  line,
			Col:   0,
			Title: fmt.Sprintf("$(layers) Overrides package view (%s)", filepath.Base(pl.File)),
			File:  pl.File,
			TLine: pl.Line,
			TCol:  pl.Col,
		})
	} else if filepath.Clean(vi.Loc.File) != cleanPath {
		lenses = append(lenses, TemplateLens{
			Line:  line,
			Col:   0,
			Title: fmt.Sprintf("$(warning) Overridden by app view (resources/views/%s)", vi.Name),
			File:  vi.Loc.File,
			TLine: vi.Loc.Line,
			TCol:  vi.Loc.Col,
		})
	}

	// Orphaned view indicator if not called by any controller/route and not included by any template
	isPartialDir := strings.HasPrefix(vi.Name, "layouts/") || strings.HasPrefix(vi.Name, "partials/") || strings.HasPrefix(vi.Name, "components/")
	if len(vi.Callers) == 0 && includedCount == 0 && !isPartialDir {
		lenses = append(lenses, TemplateLens{
			Line:  line,
			Col:   0,
			Title: "$(info) Unreferenced view (0 callers in controllers or routes)",
			File:  path,
			TLine: line,
			TCol:  0,
		})
	}
	return lenses
}

// DocSymbol represents a hierarchical document symbol for textDocument/documentSymbol.
type DocSymbol struct {
	Name      string
	Detail    string
	Kind      int // 12=Function, 2=Module, 13=Variable, 5=Class
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Children  []DocSymbol
}

// TemplateDocumentSymbols returns an outline of {{ define }}, {{ block }}, {{ template }}, {{ range }}, {{ with }}, and $vars in a .tmpl file.
func TemplateDocumentSymbols(path, src string) []DocSymbol {
	if !IsTemplateFile(path) {
		return nil
	}
	var symbols []DocSymbol
	matches := tmplActionRe.FindAllStringSubmatchIndex(src, -1)
	for _, m := range matches {
		bStart, bEnd := m[2], m[3]
		body := strings.TrimSpace(src[bStart:bEnd])
		if strings.HasPrefix(body, "/*") {
			continue
		}
		sLine, sCol := offsetToLineCol(src, m[0])
		eLine, eCol := offsetToLineCol(src, m[1])
		if dm := tmplDefineTrimmedRe.FindStringSubmatch(body); dm != nil {
			symbols = append(symbols, DocSymbol{
				Name:      "define \"" + dm[1] + "\"",
				Detail:    "template definition",
				Kind:      2, // Module
				StartLine: sLine, StartCol: sCol, EndLine: eLine, EndCol: eCol,
			})
		} else if bm := tmplIncludeTrimmedRe.FindStringSubmatch(body); bm != nil {
			symbols = append(symbols, DocSymbol{
				Name:      bm[1] + " \"" + bm[2] + "\"",
				Detail:    strings.TrimSpace(bm[3]),
				Kind:      12, // Function
				StartLine: sLine, StartCol: sCol, EndLine: eLine, EndCol: eCol,
			})
		} else if rm := tmplScopeTrimmedRe.FindStringSubmatch(body); rm != nil {
			symbols = append(symbols, DocSymbol{
				Name:      rm[1] + " " + strings.TrimSpace(rm[2]),
				Detail:    "scope block",
				Kind:      5, // Class/Namespace
				StartLine: sLine, StartCol: sCol, EndLine: eLine, EndCol: eCol,
			})
		} else if lm := tmplLocalVarTrimmedRe.FindStringSubmatch(body); lm != nil {
			symbols = append(symbols, DocSymbol{
				Name:      lm[1],
				Detail:    strings.TrimSpace(lm[2]),
				Kind:      13, // Variable
				StartLine: sLine, StartCol: sCol, EndLine: eLine, EndCol: eCol,
			})
		}
	}
	return symbols
}

// TemplateInlayHints returns inline type/origin hints inside .tmpl files for range/with blocks and local variable declarations.
func TemplateInlayHints(path, src string, cat *indexer.Catalog) []InlayHintItem {
	if !IsTemplateFile(path) || cat == nil {
		return nil
	}
	vars := resolveViewVarsForTemplate(path, cat)
	if len(vars) == 0 {
		return nil
	}
	locals := extractLocalTemplateVars(src, len(src), vars, cat)
	var hints []InlayHintItem
	for _, lv := range locals {
		if lv.GoType != "" {
			hints = append(hints, InlayHintItem{
				Offset: lv.Offset + len(lv.Name),
				Label:  ": " + lv.GoType,
			})
		}
	}
	return hints
}

func htmlRouteAndAssetCompletions(src string, off int, cat *indexer.Catalog) []CompItem {
	if cat == nil || off <= 0 || off > len(src) {
		return nil
	}
	lineStart := strings.LastIndexByte(src[:off], '\n') + 1
	linePrefix := src[lineStart:off]

	if htmlRouteAttrPrefixRe.MatchString(linePrefix) || jsFetchRoutePrefixRe.MatchString(linePrefix) {
		seen := map[string]bool{}
		var out []CompItem
		for _, rt := range cat.Routes {
			if rt.Method == "gRPC" || rt.Path == "" || seen[rt.Method+":"+rt.Path] {
				continue
			}
			seen[rt.Method+":"+rt.Path] = true
			detail := rt.Method + " → " + rt.Handler
			if rt.View != "" {
				detail += " (renders " + rt.View + ")"
			}
			out = append(out, CompItem{
				Label:  rt.Path,
				Detail: detail,
				Kind:   18, // Reference
			})
		}
		if out != nil {
			return out
		}
	}

	if htmlAssetAttrPrefixRe.MatchString(linePrefix) && cat.Root != "" {
		pubDir := filepath.Join(cat.Root, "public")
		var out []CompItem
		_ = filepath.WalkDir(pubDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(pubDir, p)
			if err != nil {
				return nil
			}
			webPath := "/" + filepath.ToSlash(rel)
			out = append(out, CompItem{
				Label:  webPath,
				Detail: "public" + webPath,
				Kind:   17, // File
			})
			return nil
		})
		return out
	}
	return nil
}

func htmlRouteAndAssetHover(src string, off int, cat *indexer.Catalog) *HoverResult {
	if cat == nil {
		return nil
	}
	urlPath := quotedPathAtOffset(src, off)
	if urlPath == "" || !strings.HasPrefix(urlPath, "/") {
		return nil
	}
	var matchedRoutes []*indexer.RouteItem
	for _, rt := range cat.Routes {
		if rt.Path == urlPath {
			matchedRoutes = append(matchedRoutes, rt)
		}
	}
	if len(matchedRoutes) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "**Goravel Route `%s`**\n\n", urlPath)
		for _, rt := range matchedRoutes {
			line := fmt.Sprintf("- **%s** → `%s`", rt.Method, rt.Handler)
			if rt.View != "" {
				line += fmt.Sprintf(" (view: `%s`)", rt.View)
			}
			if len(rt.Middlewares) > 0 {
				line += fmt.Sprintf(" `[%s]`", strings.Join(rt.Middlewares, ", "))
			}
			fmt.Fprintf(&b, "%s (`%s:%d`)\n", line, filepath.Base(rt.Loc.File), rt.Loc.Line+1)
		}
		return &HoverResult{Markdown: b.String()}
	}
	if cat.Root != "" {
		assetFile := filepath.Join(cat.Root, "public", filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
		if st, err := os.Stat(assetFile); err == nil && !st.IsDir() {
			return &HoverResult{
				Markdown: fmt.Sprintf("**Static Asset `%s`**\n\n- File: `public%s` (%d bytes)", urlPath, urlPath, st.Size()),
			}
		}
	}
	return nil
}

func htmlRouteAndAssetDefinition(src string, off int, cat *indexer.Catalog) []indexer.Location {
	if cat == nil {
		return nil
	}
	urlPath := quotedPathAtOffset(src, off)
	if urlPath == "" || !strings.HasPrefix(urlPath, "/") {
		return nil
	}
	var locs []indexer.Location
	for _, rt := range cat.Routes {
		if rt.Path == urlPath {
			if rt.Loc.Valid() {
				locs = append(locs, rt.Loc)
			}
			if rt.CtrlLoc.Valid() {
				locs = append(locs, rt.CtrlLoc)
			}
		}
	}
	if len(locs) > 0 {
		return locs
	}
	if cat.Root != "" {
		assetFile := filepath.Join(cat.Root, "public", filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
		if st, err := os.Stat(assetFile); err == nil && !st.IsDir() {
			return []indexer.Location{{File: assetFile, Line: 0, Col: 0}}
		}
	}
	return nil
}

func checkStaticAssetLinks(src string, cat *indexer.Catalog) []Diag {
	pubDir := filepath.Join(cat.Root, "public")
	if st, err := os.Stat(pubDir); err != nil || !st.IsDir() {
		return nil
	}
	routeSet := map[string]bool{}
	for _, rt := range cat.Routes {
		routeSet[rt.Path] = true
	}
	staticExts := map[string]bool{
		".css": true, ".js": true, ".png": true, ".jpg": true, ".jpeg": true,
		".svg": true, ".ico": true, ".webp": true, ".woff": true, ".woff2": true,
	}
	var diags []Diag
	for _, m := range htmlStaticAssetRe.FindAllStringSubmatchIndex(src, -1) {
		pStart, pEnd := m[2], m[3]
		urlPath := src[pStart:pEnd]
		ext := strings.ToLower(filepath.Ext(urlPath))
		if !staticExts[ext] || routeSet[urlPath] {
			continue
		}
		diskPath := filepath.Join(pubDir, filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
		if _, err := os.Stat(diskPath); err != nil {
			diags = append(diags, Diag{
				Start:    pStart,
				End:      pEnd,
				Severity: 2, // Warning
				Message:  fmt.Sprintf("Static asset '%s' not found in public/ directory", urlPath),
			})
		}
	}
	return diags
}

func quotedPathAtOffset(src string, off int) string {
	if off < 0 || off >= len(src) {
		return ""
	}
	l, r := off, off
	for l > 0 && src[l-1] != '"' && src[l-1] != '\'' && src[l-1] != '`' && src[l-1] != '\n' {
		l--
	}
	if l == 0 || (src[l-1] != '"' && src[l-1] != '\'' && src[l-1] != '`') {
		return ""
	}
	quote := src[l-1]
	for r < len(src) && src[r] != quote && src[r] != '\n' {
		r++
	}
	if r >= len(src) || src[r] != quote {
		return ""
	}
	val := src[l:r]
	if idx := strings.IndexAny(val, "?#"); idx >= 0 {
		val = val[:idx]
	}
	return strings.TrimSpace(val)
}

func templateDirectiveAtOffset(src string, off int) (directive, target string) {
	d, t, _, _ := templateDirectiveSpanAtOffset(src, off)
	return d, t
}

func templateDirectiveSpanAtOffset(src string, off int) (directive, target string, nameStart, nameEnd int) {
	if off < 0 || off > len(src) {
		return "", "", 0, 0
	}
	openIdx := strings.LastIndex(src[:off], "{{")
	if openIdx < 0 {
		return "", "", 0, 0
	}
	closeRel := strings.Index(src[openIdx:], "}}")
	if closeRel < 0 {
		return "", "", 0, 0
	}
	closeIdx := openIdx + closeRel + 2
	if off > closeIdx {
		return "", "", 0, 0
	}
	act := src[openIdx:closeIdx]
	m := tmplIncludeActionStartRe.FindStringSubmatchIndex(act)
	if m == nil {
		return "", "", 0, 0
	}
	return act[m[2]:m[3]], act[m[4]:m[5]], openIdx + m[4], openIdx + m[5]
}

func nestedFieldAtOffset(src string, off int) (holder, field string) {
	h, f, _, _ := nestedFieldSpanAtOffset(src, off)
	return h, f
}

func nestedFieldSpanAtOffset(src string, off int) (holder, field string, start, end int) {
	if off < 0 || off > len(src) {
		return "", "", 0, 0
	}
	if off < len(src) && src[off] == '.' && off+1 < len(src) && isIdentChar(src[off+1]) {
		off++
	}
	l, r := off, off
	for l > 0 && isIdentChar(src[l-1]) {
		l--
	}
	for r < len(src) && isIdentChar(src[r]) {
		r++
	}
	if l == r || l == 0 || src[l-1] != '.' {
		return "", "", 0, 0
	}
	field = src[l:r]
	hlEnd := l - 1
	hlStart := hlEnd
	for hlStart > 0 && (isIdentChar(src[hlStart-1]) || src[hlStart-1] == '$' || src[hlStart-1] == '.') {
		hlStart--
	}
	holder = strings.TrimSpace(src[hlStart:hlEnd])
	if holder == "" || holder == "$" || holder == "." {
		return "", "", 0, 0
	}
	return holder, field, l, r
}

func dollarVarAtOffset(src string, off int) string {
	v, _, _ := dollarVarSpanAtOffset(src, off)
	return v
}

func dollarVarSpanAtOffset(src string, off int) (string, int, int) {
	if off < 0 || off > len(src) {
		return "", 0, 0
	}
	if off < len(src) && src[off] == '$' && off+1 < len(src) && isIdentChar(src[off+1]) {
		off++
	}
	l, r := off, off
	for l > 0 && isIdentChar(src[l-1]) {
		l--
	}
	for r < len(src) && isIdentChar(src[r]) {
		r++
	}
	if l > 0 && src[l-1] == '$' && l < r {
		return "$" + src[l:r], l - 1, r
	}
	return "", 0, 0
}

func dotVarSpanAtOffset(src string, off int) (name string, isRootDollar bool, start, end int) {
	if off < 0 || off > len(src) {
		return "", false, 0, 0
	}
	// If cursor is on '$' of '$.ident', advance to ident
	if off+2 < len(src) && src[off] == '$' && src[off+1] == '.' && isIdentChar(src[off+2]) {
		off += 2
	} else if off+1 < len(src) && src[off] == '.' && isIdentChar(src[off+1]) {
		// If cursor is on '.' of '.ident' or '$.ident', advance to ident
		off++
	}
	l, r := off, off
	for l > 0 && isIdentChar(src[l-1]) {
		l--
	}
	for r < len(src) && isIdentChar(src[r]) {
		r++
	}
	if l == r || l == 0 || src[l-1] != '.' {
		return "", false, 0, 0
	}
	// Make sure this is a root/scoped '.ident' or '$.ident', not a chained '.a.b'
	if l >= 2 && isIdentChar(src[l-2]) {
		return "", false, 0, 0
	}
	if l >= 2 && src[l-2] == '$' {
		return src[l:r], true, l - 2, r
	}
	return src[l:r], false, l - 1, r
}

func bareDotAtOffset(src string, off int) (int, bool) {
	if off < 0 || off > len(src) {
		return 0, false
	}
	check := func(idx int) bool {
		if idx < 0 || idx >= len(src) || src[idx] != '.' {
			return false
		}
		if idx > 0 && (isIdentChar(src[idx-1]) || src[idx-1] == '.' || src[idx-1] == '$') {
			return false
		}
		if idx+1 < len(src) && (isIdentChar(src[idx+1]) || src[idx+1] == '.') {
			return false
		}
		return true
	}
	if check(off) {
		return off, true
	}
	if check(off - 1) {
		return off - 1, true
	}
	return 0, false
}

func templateVarAtOffset(src string, off int) string {
	w, _, _ := templateVarSpanAtOffset(src, off)
	return w
}

func templateVarSpanAtOffset(src string, off int) (string, int, int) {
	if off < 0 || off > len(src) {
		return "", 0, 0
	}
	if off+1 < len(src) && src[off] == '.' && isIdentChar(src[off+1]) {
		off++
	}
	l, r := off, off
	for l > 0 && isIdentChar(src[l-1]) {
		l--
	}
	for r < len(src) && isIdentChar(src[r]) {
		r++
	}
	if l == r {
		return "", 0, 0
	}
	return src[l:r], l, r
}

// TemplateTargetAtOffset inspects a .go or .tmpl file at off and returns either a target view name or (viewName, varKey) for References/Rename.
func TemplateTargetAtOffset(path, src string, cat *indexer.Catalog, off int) (targetViewName, targetVarKey string, isSharedVar bool) {
	if IsTemplateFile(path) {
		if !isInsideTemplateAction(src, off) {
			return "", "", false
		}
		if _, tName := templateDirectiveAtOffset(src, off); tName != "" {
			return tName, "", false
		}
		word := templateVarAtOffset(src, off)
		if word == "" {
			return "", "", false
		}
		if cat != nil {
			if _, ok := cat.SharedViewVars[word]; ok {
				return "", word, true
			}
			if vi := cat.ViewForPath(path); vi != nil {
				return vi.Name, word, false
			}
		}
		return filepath.Base(path), word, false
	}
	calls, fset := findViewCalls(path, src)
	for _, vc := range calls {
		for _, ns := range vc.Names {
			if off >= ns.Start && off <= ns.End {
				return ns.Name, "", false
			}
		}
		if vc.ShareKey != nil && off >= vc.ShareKey.Start && off <= vc.ShareKey.End {
			return "", vc.ShareKey.Name, true
		}
		if vc.DataMap != nil && fset != nil && len(vc.Names) > 0 {
			for _, elt := range vc.DataMap.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if sp, ok := extractStringSpan(fset, kv.Key); ok && off >= sp.Start && off <= sp.End {
					return vc.Names[0].Name, sp.Name, false
				}
			}
		}
	}
	return "", "", false
}

// FindViewAndTemplateSpans returns all byte spans [start, end] in (fpath, content) matching targetViewName or (targetViewName, targetVarKey).
func FindViewAndTemplateSpans(fpath, content string, cat *indexer.Catalog, targetViewName, targetVarKey string, isSharedVar bool) [][2]int {
	var spans [][2]int
	if IsTemplateFile(fpath) {
		if targetViewName != "" && targetVarKey == "" {
			// Match {{ define "target" }} or {{ template "target" }} or {{ block "target" }}
			for _, m := range tmplIncludeAnyActionRe.FindAllStringSubmatchIndex(content, -1) {
				name := content[m[2]:m[3]]
				if sameViewName(name, targetViewName, cat) {
					spans = append(spans, [2]int{m[2], m[3]})
				}
			}
			return spans
		}
		if targetVarKey != "" {
			if !isSharedVar && targetViewName != "" && cat != nil {
				vi := cat.ViewForPath(fpath)
				if vi != nil && !sameViewName(vi.Name, targetViewName, cat) {
					// Check if fpath is an included partial of targetViewName
					isRelated := false
					if parent := cat.LookupView(targetViewName); parent != nil {
						for _, inc := range parent.Includes {
							if sameViewName(inc.Target, vi.Name, cat) {
								isRelated = true
								break
							}
						}
					}
					if !isRelated {
						return nil
					}
				}
			}
			spans = append(spans, findTemplateVarSpansInContent(content, targetVarKey)...)
		}
		return spans
	}

	if !strings.HasSuffix(fpath, ".go") {
		return nil
	}
	calls, fset := findViewCalls(fpath, content)
	for _, vc := range calls {
		if targetViewName != "" && targetVarKey == "" {
			for _, ns := range vc.Names {
				if sameViewName(ns.Name, targetViewName, cat) {
					// Return inner string without quotes
					spans = append(spans, [2]int{ns.Start + 1, ns.End - 1})
				}
			}
		}
		if targetVarKey != "" {
			if isSharedVar && vc.ShareKey != nil && vc.ShareKey.Name == targetVarKey {
				spans = append(spans, [2]int{vc.ShareKey.Start + 1, vc.ShareKey.End - 1})
			}
			if vc.DataMap != nil && fset != nil {
				matchesView := isSharedVar || targetViewName == ""
				if !matchesView {
					for _, ns := range vc.Names {
						if sameViewName(ns.Name, targetViewName, cat) {
							matchesView = true
							break
						}
					}
				}
				if matchesView {
					for _, elt := range vc.DataMap.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						if sp, ok := extractStringSpan(fset, kv.Key); ok && sp.Name == targetVarKey {
							spans = append(spans, [2]int{sp.Start + 1, sp.End - 1})
						}
					}
				}
			}
		}
	}
	return spans
}

func sameViewName(a, b string, cat *indexer.Catalog) bool {
	if a == b {
		return true
	}
	noExtA := strings.TrimSuffix(a, filepath.Ext(a))
	noExtB := strings.TrimSuffix(b, filepath.Ext(b))
	if noExtA == noExtB {
		return true
	}
	if cat != nil {
		va := cat.LookupView(a)
		vb := cat.LookupView(b)
		if va != nil && vb != nil && va.Name == vb.Name {
			return true
		}
	}
	return false
}

func findTemplateVarOccurrencesInFile(filePath, key string) []indexer.Location {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	src := string(b)
	var locs []indexer.Location
	for _, sp := range findTemplateVarSpansInContent(src, key) {
		line, col := offsetToLineCol(src, sp[0])
		locs = append(locs, indexer.Location{File: filePath, Line: line, Col: col})
	}
	return locs
}

func offsetToLineCol(src string, off int) (int, int) {
	if off < 0 {
		return 0, 0
	}
	if off > len(src) {
		off = len(src)
	}
	line := strings.Count(src[:off], "\n")
	lastNL := strings.LastIndexByte(src[:off], '\n')
	col := off - (lastNL + 1)
	return line, col
}

func findTemplateVarSpansInContent(src, key string) [][2]int {
	var spans [][2]int
	varRe := regexp.MustCompile(`(?:^|[\s(|,=:$])(?:\$)?\.(` + regexp.QuoteMeta(key) + `)\b`)
	for _, m := range tmplActionRe.FindAllStringSubmatchIndex(src, -1) {
		bStart, bEnd := m[2], m[3]
		body := src[bStart:bEnd]
		for _, vm := range varRe.FindAllStringSubmatchIndex(body, -1) {
			spans = append(spans, [2]int{bStart + vm[2], bStart + vm[3]})
		}
	}
	return spans
}
