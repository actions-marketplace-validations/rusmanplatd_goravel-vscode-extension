package rawsql

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rusmanplatd/goravel-lsp/internal/indexer"
)

var (
	topStmtRe       = regexp.MustCompile(`(?is)^\s*(SELECT|INSERT|UPDATE|DELETE|WITH)\b`)
	danglingCommaRe = regexp.MustCompile(`(?i),\s*(?:\b(?:FROM|WHERE|GROUP\s+BY|ORDER\s+BY|HAVING|LIMIT|OFFSET|SET|VALUES|RETURNING|UNION|EXCEPT|INTERSECT)\b|\)|$)`)
	emptySelectRe   = regexp.MustCompile(`(?is)\bSELECT\s+(?:DISTINCT\s+)?FROM\b`)
	trailingClause  = regexp.MustCompile(`(?is)\b(SELECT|FROM|WHERE|JOIN|ON|SET|VALUES|GROUP\s+BY|ORDER\s+BY|HAVING|LIMIT|OFFSET)\s*$`)
	doubleCommaRe   = regexp.MustCompile(`,\s*,`)
	leadingCommaRe  = regexp.MustCompile(`(?is)(?:\bSELECT(?:\s+DISTINCT)?|\()\s*,`)
	dupOpRe         = regexp.MustCompile(`(?is)\b(AND\s+AND|OR\s+OR|AND\s+OR|OR\s+AND)\b`)
)

func checkSQLSyntax(sql string, dynamic bool) []Issue {
	var out []Issue
	if strings.TrimSpace(sql) == "" {
		return nil
	}

	noComments := maskSQLComments(sql)

	// 1. Check balanced quotes and parentheses
	inSingle := false
	inDouble := false
	quoteStart := -1
	var parenStack []int

	for i := 0; i < len(noComments); i++ {
		c := noComments[i]
		if inSingle {
			if c == '\'' {
				if i+1 < len(noComments) && noComments[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if inDouble {
			if c == '"' {
				if i+1 < len(noComments) && noComments[i+1] == '"' {
					i++
					continue
				}
				inDouble = false
			}
			continue
		}
		switch c {
		case '\'':
			inSingle = true
			quoteStart = i
		case '"':
			inDouble = true
			quoteStart = i
		case '(':
			parenStack = append(parenStack, i)
		case ')':
			if len(parenStack) == 0 {
				out = append(out, Issue{i, i + 1, 1, "SQL syntax error: unexpected closing parenthesis ')'"})
			} else {
				parenStack = parenStack[:len(parenStack)-1]
			}
		}
	}
	if inSingle && quoteStart >= 0 {
		out = append(out, Issue{quoteStart, quoteStart + 1, 1, "SQL syntax error: unclosed string literal"})
	}
	if inDouble && quoteStart >= 0 {
		out = append(out, Issue{quoteStart, quoteStart + 1, 1, "SQL syntax error: unclosed quoted identifier"})
	}

	// Strip string literals and comments for structural regex checks
	clean := maskStringLiterals(sql)

	isFullStmt := topStmtRe.MatchString(clean)
	if isFullStmt && !dynamic && len(parenStack) > 0 {
		p := parenStack[len(parenStack)-1]
		out = append(out, Issue{p, p + 1, 1, "SQL syntax error: unclosed parenthesis '('"})
	}

	if m := danglingCommaRe.FindStringIndex(clean); m != nil {
		out = append(out, Issue{m[0], m[0] + 1, 1, "SQL syntax error: dangling comma"})
	}
	if m := doubleCommaRe.FindStringIndex(clean); m != nil {
		out = append(out, Issue{m[0], m[1], 1, "SQL syntax error: consecutive commas"})
	}
	if m := leadingCommaRe.FindStringIndex(clean); m != nil {
		out = append(out, Issue{m[1] - 1, m[1], 1, "SQL syntax error: unexpected comma"})
	}
	if m := dupOpRe.FindStringIndex(clean); m != nil {
		out = append(out, Issue{m[0], m[1], 1, "SQL syntax error: consecutive logical operators"})
	}
	if isFullStmt {
		if m := emptySelectRe.FindStringIndex(clean); m != nil {
			out = append(out, Issue{m[0], m[1], 1, "SQL syntax error: empty SELECT list before FROM"})
		}
		if !dynamic {
			if m := trailingClause.FindStringSubmatchIndex(clean); m != nil {
				out = append(out, Issue{m[2], m[3], 1, fmt.Sprintf("SQL syntax error: incomplete %s clause", strings.ToUpper(clean[m[2]:m[3]]))})
			}
		}
	}

	return out
}

func maskSQLComments(sql string) string {
	b := []byte(sql)
	inSingle := false
	inDouble := false
	for i := 0; i < len(b); i++ {
		if inSingle {
			if b[i] == '\'' {
				if i+1 < len(b) && b[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if inDouble {
			if b[i] == '"' {
				if i+1 < len(b) && b[i+1] == '"' {
					i++
					continue
				}
				inDouble = false
			}
			continue
		}
		if b[i] == '\'' {
			inSingle = true
			continue
		}
		if b[i] == '"' {
			inDouble = true
			continue
		}
		// Line comment -- ...
		if b[i] == '-' && i+1 < len(b) && b[i+1] == '-' {
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
			continue
		}
		// Block comment /* ... */
		if b[i] == '/' && i+1 < len(b) && b[i+1] == '*' {
			b[i], b[i+1] = ' ', ' '
			i += 2
			for i < len(b) {
				if b[i] == '*' && i+1 < len(b) && b[i+1] == '/' {
					b[i], b[i+1] = ' ', ' '
					i++
					break
				}
				if b[i] != '\n' {
					b[i] = ' '
				}
				i++
			}
		}
	}
	return string(b)
}

func maskStringLiterals(sql string) string {
	b := []byte(maskSQLComments(sql))
	inSingle := false
	for i := 0; i < len(b); i++ {
		if inSingle {
			if b[i] == '\'' {
				if i+1 < len(b) && b[i+1] == '\'' {
					b[i], b[i+1] = ' ', ' '
					i++
					continue
				}
				inSingle = false
				continue
			}
			if b[i] != '\n' {
				b[i] = ' '
			}
			continue
		}
		if b[i] == '\'' {
			inSingle = true
		}
	}
	return string(b)
}

var sqlNonColumnWords = map[string]bool{}

func init() {
	words := "select distinct from where join left right inner outer full cross natural on using " +
		"and or not in is null true false as into values update set delete returning " +
		"group by order asc desc nulls first last limit offset having union all except intersect " +
		"with recursive case when then else end cast like ilike similar to between exists any some " +
		"over partition filter within without extract epoch from interval current_timestamp current_date " +
		"current_time localtimestamp default excluded conflict do nothing coalesce nullif " +
		"at time zone precision varying row rows only next ties others unbounded preceding following current respect ignore lateral ordinality " +
		"doy dow isodow isoyear year month day hour minute second week quarter century decade millennium milliseconds microseconds timezone timezone_hour timezone_minute " +
		"count sum avg min max now lower upper trim btrim ltrim rtrim length substr substring " +
		"concat replace split_part to_char to_date to_timestamp date_trunc age row_number rank dense_rank " +
		"int integer bigint smallint serial bigserial text varchar char boolean bool float double " +
		"numeric decimal real date time timestamp timestamptz uuid json jsonb bytea " +
		"for share update skip locked wait nowait fetch"
	for _, w := range strings.Fields(words) {
		sqlNonColumnWords[w] = true
	}
}

var (
	identRe     = regexp.MustCompile(`[A-Za-z_]\w*`)
	asAliasRe   = regexp.MustCompile(`(?i)\bAS\s+"?([A-Za-z_]\w*)"?`)
	whereClause = regexp.MustCompile(`(?is)\b(?:WHERE|ORDER\s+BY|GROUP\s+BY|HAVING)\b`)
	selectKwRe  = regexp.MustCompile(`(?i)\bSELECT\b(?:\s+DISTINCT\b)?`)
)

func isNonColumnIdent(low string) bool {
	return sqlNonColumnWords[low] || sqlKeywords[low] || sqlFunctions[low] || sqlTypes[low]
}

func buildLocalBodyScope(body string, base Scope, cat *indexer.Catalog) (Scope, bool) {
	local := Scope{}
	clean := maskStringLiterals(body)
	hasUnknown := false
	for _, idx := range aliasRe.FindAllStringSubmatchIndex(clean, -1) {
		if isFalseAliasKeywordMatch(clean, idx[0]) {
			continue
		}
		tbl := body[idx[2]:idx[3]]
		low := strings.ToLower(indexer.StripSchema(tbl))
		if strings.HasPrefix(tbl, "__") || strings.Contains(tbl, "__") || isSystemTable(tbl) {
			hasUnknown = true
			continue
		}
		if notAlias[low] {
			continue
		}
		after := strings.TrimSpace(clean[idx[3]:])
		if strings.HasPrefix(after, "(") {
			hasUnknown = true
			continue
		}
		var t *indexer.Table
		if cat != nil {
			t = cat.Table(tbl)
		}
		if t == nil {
			t = base[low]
		}
		if t == nil {
			hasUnknown = true
			continue
		}
		local[low] = t
		if idx[4] >= 0 {
			alias := strings.ToLower(body[idx[4]:idx[5]])
			if !notAlias[alias] {
				local[alias] = t
			}
		}
	}
	return local, hasUnknown
}

func checkUnqualifiedColumns(sql string, sc Scope, cat *indexer.Catalog) []Issue {
	if strings.Contains(strings.ToUpper(maskStringLiterals(sql)), "WITH ") {
		ctes, spans, mainStart := extractCTESpans(sql, sc, cat)
		if len(spans) == 0 {
			return nil
		}
		var out []Issue
		prior := Scope{}
		for k, t := range sc {
			prior[k] = t
		}
		for _, sp := range spans {
			body := sql[sp.bodyStart:sp.bodyEnd]
			local, hasUnknown := buildLocalBodyScope(body, prior, cat)
			if !hasUnknown && len(local.Tables()) > 0 {
				out = append(out, checkQueryBodyUnqualified(body, sp.bodyStart, local)...)
			}
			if sp.tbl != nil {
				prior[strings.ToLower(sp.name)] = sp.tbl
			}
		}
		if mainStart > 0 && mainStart < len(sql) {
			mainBody := sql[mainStart:]
			mainBase := Scope{}
			for k, t := range prior {
				mainBase[k] = t
			}
			for k, t := range ctes {
				mainBase[k] = t
			}
			local, hasUnknown := buildLocalBodyScope(mainBody, mainBase, cat)
			if !hasUnknown && len(local.Tables()) > 0 {
				out = append(out, checkQueryBodyUnqualified(mainBody, mainStart, local)...)
			}
		}
		return out
	}
	return checkQueryBodyUnqualified(sql, 0, sc)
}

// findTopLevelFrom finds the index of the top-level FROM keyword after start, or -1.
func findTopLevelFrom(clean string, start int) int {
	depth := 0
	for i := start; i < len(clean); i++ {
		switch clean[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && i+4 <= len(clean) && strings.EqualFold(clean[i:i+4], "FROM") {
				prevOK := i == 0 || !isWord(clean[i-1])
				nextOK := i+4 == len(clean) || !isWord(clean[i+4])
				if prevOK && nextOK {
					return i
				}
			}
		}
	}
	return -1
}

func checkQueryBodyUnqualified(sql string, baseOffset int, sc Scope) []Issue {
	clean := maskStringLiterals(sql)

	// Collect explicit AS aliases from SELECT so ORDER BY / GROUP BY alias isn't flagged
	aliases := map[string]bool{}
	for _, m := range asAliasRe.FindAllStringSubmatch(clean, -1) {
		aliases[strings.ToLower(m[1])] = true
	}
	// Also collect table names/aliases in scope
	tables := sc.Tables()
	hasCol := func(name string) bool {
		for _, t := range tables {
			if t.Column(name) != nil {
				return true
			}
		}
		return false
	}

	var out []Issue
	seenPos := map[int]bool{}
	scanRange := func(start, end int) {
		if start < 0 || end <= start || end > len(clean) {
			return
		}
		sub := clean[start:end]
		for _, m := range identRe.FindAllStringIndex(sub, -1) {
			absStart := start + m[0]
			absEnd := start + m[1]
			if seenPos[absStart] {
				continue
			}
			word := sub[m[0]:m[1]]
			low := strings.ToLower(word)

			if strings.HasPrefix(word, "__") || isNonColumnIdent(low) || aliases[low] || sc[low] != nil {
				continue
			}
			if absStart > 0 {
				prev := clean[absStart-1]
				if prev == '.' || prev == '$' || prev == ':' || prev == '\'' || prev == '"' {
					continue
				}
			}
			if absEnd < len(clean) {
				next := clean[absEnd]
				if next == '.' || next == '(' {
					continue
				}
			}
			if hasCol(word) {
				continue
			}
			seenPos[absStart] = true
			msg := fmt.Sprintf("Column '%s' does not exist on table(s) in scope", word)
			if len(tables) == 1 {
				if sug := closestColumn(tables[0], word); sug != "" {
					msg += fmt.Sprintf(" (did you mean '%s'?)", sug)
				}
			}
			out = append(out, Issue{baseOffset + absStart, baseOffset + absEnd, 1, msg})
		}
	}

	// 1. Check top-level SELECT list (including expressions and function arguments like json_build_object)
	if sm := selectKwRe.FindStringIndex(clean); sm != nil {
		fromIdx := findTopLevelFrom(clean, sm[1])
		if fromIdx > sm[1] {
			scanRange(sm[1], fromIdx)
		}
	}

	// 2. Check WHERE / ORDER BY / GROUP BY / HAVING tail
	if wm := whereClause.FindStringIndex(clean); wm != nil {
		scanRange(wm[0], len(clean))
	}
	return out
}

