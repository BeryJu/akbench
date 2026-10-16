package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The trace breakdown groups database spans by statement. A span's
// db.statement is the SQL Django handed psycopg, which differs between calls
// of the same query in ways that don't matter here: authentik's sqlcommenter
// appends the route, controller and calling span's traceparent to every
// query, an IN list is as long as its argument, and Django names each
// savepoint after its thread. normalizeStatement reduces a statement to its
// shape, statementMatchKey finds that shape among pg_stat_statements' texts
// (pgstats.go), and summarizeStatement shortens it for display.

var (
	// djangoSavepoint is the name Django's savepoint() generates,
	// s<thread id>_x<counter>. Postgres records it as a placeholder too.
	djangoSavepoint = regexp.MustCompile(`^"s\d+_x\d+"$`)
	// placeholderList is two or more comma-separated placeholders.
	placeholderList = regexp.MustCompile(`%s(?: ?, ?%s)+`)
	// placeholderRows is two or more parenthesized placeholder lists, as in
	// a bulk INSERT's VALUES.
	placeholderRows = regexp.MustCompile(`\(%s(?:, …)?\)(?: ?, ?\(%s(?:, …)?\))+`)
	// placeholderCast is a cast on a placeholder. psycopg's client-side
	// binding sends some values with one ('…'::timestamptz), which
	// pg_stat_statements keeps as $1::timestamptz.
	placeholderCast = regexp.MustCompile(`%s(?:::(?:"[^"]*"|[A-Za-z_][A-Za-z0-9_.]*)(?:\[\])*)+`)
	// bareConstant is a literal Django writes out but Postgres records as a
	// placeholder, or the other way around: psycopg sends None as NULL.
	bareConstant = regexp.MustCompile(`(?i)\b(?:null|true|false)\b`)
)

// normalizeStatement reduces a statement to its shape:
//
//   - comments are dropped
//   - placeholders (Django's %s, Postgres's $1), literals and Django's
//     savepoint names become %s, and a list of them becomes "%s, …"
//   - runs of whitespace become one space, or none after "(" and before ","
//     or ")"
func normalizeStatement(sql string) string {
	return collapsePlaceholders(normalizeTokens(sql))
}

// normalizeTokens is normalizeStatement without collapsing lists.
func normalizeTokens(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	space := false
	var last byte
	for i := 0; i < len(sql); {
		var token string
		token, i = scanToken(sql, i)
		if token == "" {
			space = true
			continue
		}
		if space && b.Len() > 0 && last != '(' && token != "," && token != ")" {
			b.WriteByte(' ')
		}
		space = false
		b.WriteString(token)
		last = token[len(token)-1]
	}
	return b.String()
}

// scanToken reads the token starting at i: "" for whitespace or a comment,
// %s for a value, otherwise its text.
func scanToken(sql string, i int) (token string, end int) {
	c := sql[i]
	switch {
	case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
		return "", i + 1
	case strings.HasPrefix(sql[i:], "/*"):
		return "", skipBlockComment(sql, i)
	case strings.HasPrefix(sql[i:], "--"):
		if n := strings.IndexByte(sql[i:], '\n'); n >= 0 {
			return "", i + n
		}
		return "", len(sql)
	case c == '\'':
		return "%s", skipQuoted(sql, i)
	case c == '"':
		end = skipQuoted(sql, i)
		if djangoSavepoint.MatchString(sql[i:end]) {
			return "%s", end
		}
		return sql[i:end], end
	case strings.HasPrefix(sql[i:], "%s"):
		return "%s", i + 2
	case strings.HasPrefix(sql[i:], "%%"):
		return "%", i + 2
	case c == '$':
		var value bool
		end, value = scanDollar(sql, i)
		if value {
			return "%s", end
		}
		return sql[i:end], end
	case isDigit(c):
		return "%s", skipNumber(sql, i)
	case isIdentStart(c):
		return scanWord(sql, i)
	}
	return sql[i : i+1], i + 1
}

// scanWord reads the identifier or keyword starting at i, or the string
// literal it prefixes: E'…' (with backslash escapes), B'…', X'…' or N'…'.
func scanWord(sql string, i int) (token string, end int) {
	end = i + 1
	for end < len(sql) && isIdentPart(sql[end]) {
		end++
	}
	if end < len(sql) && sql[end] == '\'' {
		switch strings.ToUpper(sql[i:end]) {
		case "E":
			return "%s", skipEscapedString(sql, end)
		case "B", "X", "N":
			return "%s", skipQuoted(sql, end)
		}
	}
	return sql[i:end], end
}

func collapsePlaceholders(s string) string {
	s = placeholderList.ReplaceAllString(s, "%s, …")
	return placeholderRows.ReplaceAllStringFunc(s, func(rows string) string {
		return rows[:strings.IndexByte(rows, ')')+1] + ", …"
	})
}

// statementMatchKey goes further than normalizeStatement, far enough to find
// a traced statement among pg_stat_statements' texts. Postgres saw psycopg's
// client-side rendering of Django's statement, so a value may carry a cast
// ($4::bytea), None arrives as NULL, and Postgres 18 squashes an IN list to
// "($1 /*, ... */)". So casts on a placeholder go, NULL, true and false
// become placeholders, any list of them becomes a single one, and
// whitespace goes.
func statementMatchKey(sql string) string {
	key := placeholderCast.ReplaceAllString(normalizeTokens(sql), "%s")
	key = bareConstant.ReplaceAllString(key, "%s")
	key = placeholderList.ReplaceAllString(key, "%s")
	key = placeholderRows.ReplaceAllString(key, "(%s)")
	return strings.TrimSuffix(strings.Join(strings.Fields(key), ""), ";")
}

// statementID is a short, stable name for a normalized statement.
func statementID(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}

const (
	// summaryColumnsMin is how long a SELECT's column list must be before
	// summarizeStatement drops it. Shorter ones, like exists()'s
	// "SELECT %s AS "a"" or count()'s, say what the query is for.
	summaryColumnsMin = 80
	// summaryJoinsMax is how many joins summarizeStatement keeps in full.
	summaryJoinsMax = 2

	keywordSelect = "SELECT"
)

var (
	joinWords   = map[string]bool{"JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true, "FULL": true, "CROSS": true, "NATURAL": true}
	clauseWords = map[string]bool{
		"WHERE": true, "GROUP": true, "HAVING": true, "ORDER": true, "LIMIT": true, "OFFSET": true, "FOR": true,
		"UNION": true, "INTERSECT": true, "EXCEPT": true, "RETURNING": true, "WINDOW": true, keywordSelect: true,
	}
)

// summarizeStatement shortens a normalized statement for display: a
// top-level SELECT's long column list becomes "…" and a chain of more than
// summaryJoinsMax joins becomes "… N joins …". What's left are the tables,
// the filter and the order, which decide whether and how to optimize it.
func summarizeStatement(normalized string) string {
	type cut struct {
		start, end int
		with       string
	}
	var cuts []cut
	words := topLevelWords(normalized)
	for i, w := range words {
		switch w.word {
		case keywordSelect:
			start := w.end
			if i+1 < len(words) && words[i+1].word == "DISTINCT" {
				start = words[i+1].end
			}
			for _, next := range words[i+1:] {
				if next.word == keywordSelect {
					break
				}
				if next.word == "FROM" {
					if next.start-start > summaryColumnsMin {
						cuts = append(cuts, cut{start, next.start, " … "})
					}
					break
				}
			}
		case "FROM":
			first, end, joins := -1, len(normalized), 0
			for _, next := range words[i+1:] {
				if clauseWords[next.word] {
					end = next.start
					break
				}
				if joinWords[next.word] && first < 0 {
					first = next.start
				}
				if next.word == "JOIN" {
					joins++
				}
			}
			if joins > summaryJoinsMax {
				cuts = append(cuts, cut{first, end, fmt.Sprintf("… %d joins … ", joins)})
			}
		}
	}
	var b strings.Builder
	last := 0
	for _, c := range cuts {
		b.WriteString(normalized[last:c.start])
		b.WriteString(c.with)
		last = c.end
	}
	b.WriteString(normalized[last:])
	return strings.TrimSpace(b.String())
}

// truncateStatement cuts s to at most n bytes, on a character boundary.
func truncateStatement(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

type sqlWord struct {
	// word is upper-cased.
	word       string
	start, end int
}

// topLevelWords are the words of s outside parentheses and quoted
// identifiers - its clauses' keywords, among others.
func topLevelWords(s string) []sqlWord {
	var words []sqlWord
	depth := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			i = skipQuoted(s, i)
		case c == '(':
			depth++
			i++
		case c == ')':
			depth--
			i++
		case isIdentStart(c):
			end := i + 1
			for end < len(s) && isIdentPart(s[end]) {
				end++
			}
			if depth == 0 {
				words = append(words, sqlWord{word: strings.ToUpper(s[i:end]), start: i, end: end})
			}
			i = end
		default:
			i++
		}
	}
	return words
}

// skipQuoted returns the end of the quoted string or identifier starting at
// i, where a doubled quote is an escaped one.
func skipQuoted(s string, i int) int {
	quote := s[i]
	for j := i + 1; j < len(s); j++ {
		if s[j] != quote {
			continue
		}
		if j+1 < len(s) && s[j+1] == quote {
			j++
			continue
		}
		return j + 1
	}
	return len(s)
}

// skipEscapedString returns the end of the E'…' string whose quote is at i,
// where a backslash escapes the character after it.
func skipEscapedString(s string, i int) int {
	for j := i + 1; j < len(s); j++ {
		switch {
		case s[j] == '\\':
			j++
		case s[j] == '\'' && j+1 < len(s) && s[j+1] == '\'':
			j++
		case s[j] == '\'':
			return j + 1
		}
	}
	return len(s)
}

// skipBlockComment returns the end of the /* comment */ starting at i.
// Postgres nests them.
func skipBlockComment(s string, i int) int {
	depth := 0
	for j := i; j < len(s); {
		switch {
		case strings.HasPrefix(s[j:], "/*"):
			depth++
			j += 2
		case strings.HasPrefix(s[j:], "*/"):
			depth--
			j += 2
			if depth == 0 {
				return j
			}
		default:
			j++
		}
	}
	return len(s)
}

// scanDollar reads what starts with $ at i: a positional placeholder ($1) or
// a dollar-quoted string ($$…$$, $tag$…$tag$), both values, or a lone $.
func scanDollar(s string, i int) (end int, value bool) {
	j := i + 1
	if j < len(s) && isDigit(s[j]) {
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		return j, true
	}
	for j < len(s) && s[j] != '$' && isIdentPart(s[j]) {
		j++
	}
	if j < len(s) && s[j] == '$' {
		tag := s[i : j+1]
		if k := strings.Index(s[j+1:], tag); k >= 0 {
			return j + 1 + k + len(tag), true
		}
		return len(s), true
	}
	return i + 1, false
}

// skipNumber returns the end of the numeric literal starting at i.
func skipNumber(s string, i int) int {
	j := i
	for j < len(s) && isDigit(s[j]) {
		j++
	}
	if j+1 < len(s) && s[j] == '.' && isDigit(s[j+1]) {
		j++
		for j < len(s) && isDigit(s[j]) {
			j++
		}
	}
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		if k < len(s) && isDigit(s[k]) {
			j = k
			for j < len(s) && isDigit(s[j]) {
				j++
			}
		}
	}
	return j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isIdentStart reports whether c can start an unquoted identifier or
// keyword. Bytes of a multi-byte character count, as in Postgres.
func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) || c == '$' }
