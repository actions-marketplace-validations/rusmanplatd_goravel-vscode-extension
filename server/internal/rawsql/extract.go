// Package rawsql extracts raw SQL from Go source (including string concatenation
// and fmt.Sprintf) into virtual SQL documents with a bidirectional offset map.
package rawsql

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

// Seg maps a run of virtual SQL bytes back to Go source bytes (literal text only).
type Seg struct {
	VStart, VEnd int
	GoStart      int
}

// VarRef records a Go table-name variable interpolated into a SQL Doc.
type VarRef struct {
	GoStart int
	GoEnd   int
	Table   string
}

// Doc is one reconstructed SQL statement/fragment.
type Doc struct {
	Text    string
	Segs    []Seg
	VarRefs []VarRef
	Func    int // offset of enclosing func (or -1)
	GoStart int
	GoEnd   int
	Dynamic bool // contains values only known at runtime (format verbs, unknown vars)
}

// FileInfo is the extraction result for one Go file.
type FileInfo struct {
	Src       []byte
	Docs      []*Doc
	Queries   []*QueryCall
	TableVars map[string]string // go var name -> table name
}

var (
	sqlish = regexp.MustCompile(`(?is)^\s*(SELECT|INSERT\s+INTO|UPDATE|DELETE\s+FROM|WITH|CREATE|ALTER|DROP|TRUNCATE|AND|OR|WHERE|ORDER\s+BY|GROUP\s+BY|HAVING|LIMIT|OFFSET|LEFT\s+JOIN|INNER\s+JOIN|RIGHT\s+JOIN|JOIN|SET|FROM|ON)\s`)

	standaloneSQLRe = regexp.MustCompile(`(?is)^\s*(?:` +
		`SELECT\b.+\bFROM\b|` +
		`INSERT\s+INTO\b.+\b(?:VALUES|SELECT)\b|` +
		`UPDATE\s+[A-Za-z_][\w."]*\s+(?:(?:AS\s+)?[A-Za-z_]\w*\s+)?SET\b|` +
		`DELETE\s+FROM\s+[A-Za-z_]|` +
		`WITH\b.+\bAS\s*\(|` +
		`(?:CREATE|ALTER|DROP|TRUNCATE)\s+(?:OR\s+REPLACE\s+)?(?:TABLE|INDEX|UNIQUE\s+INDEX|VIEW|MATERIALIZED\s+VIEW|FUNCTION|PROCEDURE|TRIGGER|POLICY|SCHEMA|EXTENSION|TYPE|SEQUENCE|DEFAULT\s+PRIVILEGES)\b` +
		`)`)

	clauseFragmentSQLRe = regexp.MustCompile(`(?is)^\s*(?:` +
		`(?:WHERE|AND|OR|HAVING|ON)\s+.+(?:=|<|>|\bIS\s+(?:NOT\s+)?NULL\b|\bIN\s*\(|\bLIKE\b|\bILIKE\b|\bBETWEEN\b|\$\d+|\?)|` +
		`(?:LEFT\s+|RIGHT\s+|INNER\s+|FULL\s+|CROSS\s+)?JOIN\s+.+\bON\b|` +
		`ORDER\s+BY\s+[A-Za-z_][\w.]*(?:\s+(?:ASC|DESC|NULLS\s+(?:FIRST|LAST)))?\s*(?:,|$)|` +
		`GROUP\s+BY\s+[A-Za-z_][\w.]*\s*(?:,|$)|` +
		`LIMIT\s+(?:\$\d+|\?|\d+)|` +
		`OFFSET\s+(?:\$\d+|\?|\d+)` +
		`)`)
)

// proseFromRe matches "SELECT ... FROM <article/pronoun>" which is English prose
// (e.g. "select a thing from the menu"), not SQL.
var proseFromRe = regexp.MustCompile(`(?is)^\s*SELECT\b[^;]*?\bFROM\s+(?:the|a|an|this|that|these|those|my|your|our|its|his|her|their|some|any|each|every)\b`)

func isStandaloneOrFragmentSQL(text string) bool {
	clean := maskSQLComments(text)
	if proseFromRe.MatchString(clean) {
		return false
	}
	return standaloneSQLRe.MatchString(clean) || clauseFragmentSQLRe.MatchString(clean)
}

// Extract parses Go source and returns its SQL docs. Table-name variables are
// resolved via the catalog (x := (models.T{}).TableName(ctx)).
func Extract(path string, src []byte, cat *indexer.Catalog) *FileInfo {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	fi := &FileInfo{Src: src, TableVars: map[string]string{}}
	if f == nil {
		return fi
	}
	tf := fset.File(f.Pos())
	if tf == nil {
		return fi
	}
	off := func(p token.Pos) int { return tf.Offset(p) }

	// variable -> table name
	vars := fi.TableVars
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		id, ok := as.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		if tname := extractTableNameExpr(as.Rhs[0], cat); tname != "" {
			vars[id.Name] = tname
		}
		return true
	})

	resolveSprintfArgTable := func(arg ast.Expr) string {
		if id, ok := arg.(*ast.Ident); ok {
			if t, exists := vars[id.Name]; exists {
				return t
			}
		}
		if tname := extractTableNameExpr(arg, cat); tname != "" {
			return tname
		}
		return ""
	}

	build := func(e ast.Expr, fn int, env map[string][]ast.Expr) *Doc {
		d := &Doc{Func: fn, GoStart: off(e.Pos()), GoEnd: off(e.End())}
		var sb strings.Builder
		phIdx := 1
		var sprintfTables []string
		var flat func(e ast.Expr, depth int, inSprintf bool)
		flat = func(e ast.Expr, depth int, inSprintf bool) {
			switch x := e.(type) {
			case *ast.BasicLit:
				if x.Kind != token.STRING {
					sb.WriteString("__")
					return
				}
				start := off(x.Pos()) + 1
				end := off(x.End()) - 1
				if end < start {
					return
				}
				raw := string(src[start:end])
				if inSprintf {
					appendSprintfLit(&sb, &d.Segs, raw, start, &phIdx, &d.Dynamic, sprintfTables)
				} else {
					vs := sb.Len()
					sb.WriteString(neutralize(raw))
					d.Segs = append(d.Segs, Seg{vs, sb.Len(), start})
				}
			case *ast.ParenExpr:
				flat(x.X, depth, inSprintf)
			case *ast.BinaryExpr:
				if x.Op == token.ADD {
					flat(x.X, depth, inSprintf)
					flat(x.Y, depth, inSprintf)
				} else {
					d.Dynamic = true
					sb.WriteString("__")
				}
			case *ast.Ident:
				if t, ok := vars[x.Name]; ok {
					d.VarRefs = append(d.VarRefs, VarRef{GoStart: off(x.Pos()), GoEnd: off(x.End()), Table: t})
					sb.WriteString(t)
				} else if exprs := env[x.Name]; len(exprs) > 0 && depth < 8 {
					for _, ex := range exprs {
						flat(ex, depth+1, inSprintf)
					}
				} else {
					d.Dynamic = true
					sb.WriteString("__" + x.Name + "__")
				}
			case *ast.CallExpr:
				if isSprintf(x) && len(x.Args) > 0 {
					d.Dynamic = true
					prev := sprintfTables
					sprintfTables = make([]string, 0, len(x.Args)-1)
					for _, arg := range x.Args[1:] {
						sprintfTables = append(sprintfTables, resolveSprintfArgTable(arg))
					}
					flat(x.Args[0], depth, true)
					sprintfTables = prev
				} else if tname := extractTableNameExpr(x, cat); tname != "" {
					sb.WriteString(tname)
				} else {
					d.Dynamic = true
					sb.WriteString("__call__")
				}
			default:
				d.Dynamic = true
				sb.WriteString("__")
			}
		}
		flat(e, 0, false)
		d.Text = sb.String()
		return d
	}

	seen := map[[2]int]bool{}
	addDoc := func(d *Doc) {
		k := [2]int{d.GoStart, d.GoEnd}
		if seen[k] {
			return
		}
		seen[k] = true
		fi.Docs = append(fi.Docs, d)
	}

	handle := func(root ast.Node, fn int) {
		ast.Inspect(root, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, lhs := range x.Lhs {
					if i >= len(x.Rhs) {
						break
					}
					if id, ok := lhs.(*ast.Ident); ok && isSQLVarName(id.Name) {
						if d := build(x.Rhs[i], fn, nil); len(d.Segs) > 0 && (sqlish.MatchString(d.Text) || isStandaloneOrFragmentSQL(d.Text)) {
							addDoc(d)
						}
					}
				}
			case *ast.ValueSpec:
				for i, id := range x.Names {
					if i >= len(x.Values) {
						break
					}
					if isSQLVarName(id.Name) {
						if d := build(x.Values[i], fn, nil); len(d.Segs) > 0 && (sqlish.MatchString(d.Text) || isStandaloneOrFragmentSQL(d.Text)) {
							addDoc(d)
						}
					}
				}
			case *ast.BinaryExpr:
				if x.Op != token.ADD {
					return true
				}
				if d := build(x, fn, nil); len(d.Segs) > 0 && anySQL(d, src) {
					addDoc(d)
					return false
				}
			case *ast.CallExpr:
				if isSprintf(x) && len(x.Args) > 0 {
					if d := build(x, fn, nil); len(d.Segs) > 0 && isStandaloneOrFragmentSQL(d.Text) {
						addDoc(d)
						return false
					}
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					if d := build(x, fn, nil); isStandaloneOrFragmentSQL(d.Text) {
						addDoc(d)
					}
				}
			}
			return true
		})
	}
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
			handle(fd.Body, off(fd.Pos()))
			analyzeQueries(fi, fd, cat, off, build)
		} else {
			handle(decl, -1)
		}
	}
	return fi
}

func extractTableNameExpr(e ast.Expr, cat *indexer.Catalog) string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "TableName" {
		return ""
	}
	x := sel.X
	for {
		if p, ok := x.(*ast.ParenExpr); ok {
			x = p.X
			continue
		}
		break
	}
	cl, ok := x.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	name := ""
	switch t := cl.Type.(type) {
	case *ast.Ident:
		name = t.Name
	case *ast.SelectorExpr:
		name = t.Sel.Name
	}
	if tbl := cat.Model(name); tbl != nil {
		return tbl.Name
	}
	return ""
}

func anySQL(d *Doc, src []byte) bool {
	if isStandaloneOrFragmentSQL(d.Text) {
		return true
	}
	if len(d.VarRefs) > 0 && sqlish.MatchString(d.Text) {
		return true
	}
	for _, s := range d.Segs {
		segText := string(neutralize(string(src[s.GoStart:s.GoStart+s.VEnd-s.VStart]))) + " "
		if isStandaloneOrFragmentSQL(segText) {
			return true
		}
	}
	return false
}

func isSprintf(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "fmt" && sel.Sel.Name == "Sprintf"
}

func isSQLVarName(name string) bool {
	low := strings.ToLower(name)
	return strings.Contains(low, "query") || strings.Contains(low, "sql")
}

// appendSprintfLit neutralizes a fmt.Sprintf format string literal while replacing
// %d/%s/%v format verbs with resolved table names, dynamic markers, or sequential $1..$N SQL placeholders,
// preserving Seg byte mapping.
func appendSprintfLit(sb *strings.Builder, segs *[]Seg, raw string, goStart int, phIdx *int, dynamic *bool, sprintfTables []string) {
	neut := neutralize(raw)
	n := len(raw)
	segGoStart := goStart
	segVStart := sb.Len()
	hasExplicitDollarPh := strings.Contains(raw, "$1")
	argIdx := 0

	flushSeg := func(goEnd int) {
		if sb.Len() > segVStart {
			*segs = append(*segs, Seg{VStart: segVStart, VEnd: sb.Len(), GoStart: segGoStart})
		}
		segGoStart = goEnd
	}

	for i := 0; i < n; i++ {
		if i+1 < n && raw[i] == '%' {
			verb := raw[i+1]
			if verb == '%' {
				sb.WriteString("%%")
				i++
				continue
			}
			if verb == 'd' || verb == 's' || verb == 'v' {
				*dynamic = true
				curArg := argIdx
				argIdx++
				if i > 0 && raw[i-1] == '$' {
					// e.g. $%d -> already has '$' before it; emit placeholder digit(s)
					ph := strconv.Itoa(*phIdx)
					*phIdx++
					if len(ph) == 1 {
						sb.WriteByte(ph[0])
						sb.WriteByte(' ')
					} else {
						flushSeg(goStart + i)
						sb.WriteString(ph)
						segGoStart = goStart + i + 2
						segVStart = sb.Len()
					}
					i++
					continue
				}
				if curArg < len(sprintfTables) && sprintfTables[curArg] != "" {
					flushSeg(goStart + i)
					sb.WriteString(sprintfTables[curArg])
					segGoStart = goStart + i + 2
					segVStart = sb.Len()
					i++
					continue
				}
				if hasExplicitDollarPh && (verb == 's' || verb == 'v') {
					// %s inside a query that already uses $1..$N is interpolating a dynamic table/column/fragment
					sb.WriteString("__")
					i++
					continue
				}
				ph := "$" + strconv.Itoa(*phIdx)
				*phIdx++
				if len(ph) == 2 {
					sb.WriteString(ph)
				} else {
					flushSeg(goStart + i)
					sb.WriteString(ph)
					segGoStart = goStart + i + 2
					segVStart = sb.Len()
				}
				i++
				continue
			}
		}
		sb.WriteByte(neut[i])
	}
	flushSeg(goStart + n)
}

// neutralize keeps byte length but removes escapes/format verbs so SQL scanning works.
func neutralize(s string) string {
	b := []byte(s)
	for i := 0; i+1 < len(b); i++ {
		switch {
		case b[i] == '\\' && (b[i+1] == 'n' || b[i+1] == 't' || b[i+1] == 'r'):
			b[i], b[i+1] = ' ', ' '
			i++
		case b[i] == '\\' && b[i+1] == '"':
			b[i] = ' '
			i++
		case b[i] == '%' && (b[i+1] == 'd' || b[i+1] == 's' || b[i+1] == 'v'):
			b[i], b[i+1] = '_', '_'
			i++
		}
	}
	return string(b)
}

// ToVirtual maps a Go byte offset to a virtual offset.
func (d *Doc) ToVirtual(goOff int) (int, bool) {
	for _, s := range d.Segs {
		if goOff >= s.GoStart && goOff <= s.GoStart+(s.VEnd-s.VStart) {
			return s.VStart + goOff - s.GoStart, true
		}
	}
	return 0, false
}

// ToGo maps a virtual offset to a Go offset (only if inside a literal segment).
func (d *Doc) ToGo(v int) (int, bool) {
	for _, s := range d.Segs {
		if v >= s.VStart && v <= s.VEnd {
			return s.GoStart + v - s.VStart, true
		}
	}
	return 0, false
}

// VarRefAt returns the interpolated table-name variable at Go offset, if any.
func (fi *FileInfo) VarRefAt(goOff int) *VarRef {
	for _, d := range fi.Docs {
		for i := range d.VarRefs {
			vr := &d.VarRefs[i]
			if goOff >= vr.GoStart && goOff <= vr.GoEnd {
				return vr
			}
		}
	}
	return nil
}

// DocAt returns the doc whose Go range contains offset.
func (fi *FileInfo) DocAt(goOff int) *Doc {
	var best *Doc
	for _, d := range fi.Docs {
		if goOff >= d.GoStart && goOff <= d.GoEnd {
			if _, ok := d.ToVirtual(goOff); !ok {
				continue
			}
			if best == nil || d.GoEnd-d.GoStart < best.GoEnd-best.GoStart {
				best = d
			}
		}
	}
	return best
}
