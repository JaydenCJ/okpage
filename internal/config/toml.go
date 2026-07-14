package config

// A deliberately small TOML-subset parser, so okpage keeps its zero-dependency
// promise while the config file stays human-friendly.
//
// Supported syntax:
//
//	# comments
//	key = "string"          basic strings with \\ \" \n \t \r \uXXXX escapes
//	key = 42                integers (optional leading sign)
//	key = true / false      booleans
//	key = ["a", "b"]        one-line arrays of strings or integers
//	[table]                 tables
//	[[table]]               arrays of tables
//
// Anything outside this subset (multi-line strings, floats, dates, dotted or
// quoted keys, inline tables, nested arrays) is rejected with a line-numbered
// error rather than silently misread.

import (
	"fmt"
	"strconv"
	"strings"
)

// document is the untyped parse result: scalar values, []any arrays,
// map[string]any tables, and []map[string]any arrays of tables.
type document map[string]any

// parseTOML parses src into a document. Errors always carry a 1-based line
// number so config mistakes are easy to locate.
func parseTOML(src string) (document, error) {
	doc := document{}
	// current is the table new keys land in; starts at the document root.
	current := map[string]any(doc)

	for i, raw := range strings.Split(src, "\n") {
		lineNo := i + 1
		line, err := stripComment(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", lineNo, err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "[["):
			name, err := headerName(line, "[[", "]]")
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", lineNo, err)
			}
			entry := map[string]any{}
			switch existing := doc[name].(type) {
			case nil:
				doc[name] = []map[string]any{entry}
			case []map[string]any:
				doc[name] = append(existing, entry)
			default:
				return nil, fmt.Errorf("line %d: [[%s]] conflicts with an earlier non-array value", lineNo, name)
			}
			current = entry

		case strings.HasPrefix(line, "["):
			name, err := headerName(line, "[", "]")
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", lineNo, err)
			}
			switch existing := doc[name].(type) {
			case nil:
				table := map[string]any{}
				doc[name] = table
				current = table
			case map[string]any:
				// Re-opening a table is a config smell; reject it so keys
				// cannot be scattered across the file unnoticed.
				_ = existing
				return nil, fmt.Errorf("line %d: table [%s] is defined twice", lineNo, name)
			default:
				return nil, fmt.Errorf("line %d: [%s] conflicts with an earlier value", lineNo, name)
			}

		default:
			key, value, err := parseKeyValue(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", lineNo, err)
			}
			if _, dup := current[key]; dup {
				return nil, fmt.Errorf("line %d: key %q is set twice", lineNo, key)
			}
			current[key] = value
		}
	}
	return doc, nil
}

// stripComment removes a trailing # comment, ignoring # characters that
// appear inside a quoted string.
func stripComment(line string) (string, error) {
	inString := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			if inString {
				i++ // skip the escaped character
			}
		case '"':
			inString = !inString
		case '#':
			if !inString {
				return line[:i], nil
			}
		}
	}
	if inString {
		return "", fmt.Errorf("unterminated string")
	}
	return line, nil
}

// headerName validates a [table] / [[table]] header and returns the bare name.
func headerName(line, open, close string) (string, error) {
	if !strings.HasSuffix(line, close) {
		return "", fmt.Errorf("malformed table header %q", line)
	}
	name := strings.TrimSpace(line[len(open) : len(line)-len(close)])
	if !isBareKey(name) {
		return "", fmt.Errorf("invalid table name %q (bare names only: letters, digits, - and _)", name)
	}
	return name, nil
}

// parseKeyValue parses one `key = value` line.
func parseKeyValue(line string) (string, any, error) {
	eq := strings.Index(line, "=")
	if eq < 0 {
		return "", nil, fmt.Errorf("expected key = value, got %q", line)
	}
	key := strings.TrimSpace(line[:eq])
	if !isBareKey(key) {
		return "", nil, fmt.Errorf("invalid key %q (bare names only: letters, digits, - and _)", key)
	}
	value, err := parseValue(strings.TrimSpace(line[eq+1:]))
	if err != nil {
		return "", nil, fmt.Errorf("key %q: %v", key, err)
	}
	return key, value, nil
}

// parseValue parses a scalar or one-line array value.
func parseValue(s string) (any, error) {
	switch {
	case s == "":
		return nil, fmt.Errorf("missing value")
	case s[0] == '"':
		str, rest, err := parseString(s)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(rest) != "" {
			return nil, fmt.Errorf("unexpected trailing characters %q", strings.TrimSpace(rest))
		}
		return str, nil
	case s == "true":
		return true, nil
	case s == "false":
		return false, nil
	case s[0] == '[':
		return parseArray(s)
	default:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("unsupported value %q (expected string, integer, boolean, or array)", s)
		}
		return n, nil
	}
}

// parseString parses a leading basic string and returns the decoded value
// plus whatever follows the closing quote.
func parseString(s string) (string, string, error) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return b.String(), s[i+1:], nil
		case '\\':
			if i+1 >= len(s) {
				return "", "", fmt.Errorf("dangling escape at end of string")
			}
			i++
			switch s[i] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'u':
				if i+4 >= len(s) {
					return "", "", fmt.Errorf("truncated \\u escape")
				}
				code, err := strconv.ParseUint(s[i+1:i+5], 16, 32)
				if err != nil {
					return "", "", fmt.Errorf("invalid \\u escape %q", s[i+1:i+5])
				}
				b.WriteRune(rune(code))
				i += 4
			default:
				return "", "", fmt.Errorf("unsupported escape \\%c", s[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", "", fmt.Errorf("unterminated string")
}

// parseArray parses a one-line array of strings or integers.
func parseArray(s string) (any, error) {
	if !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("unterminated array")
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return []any{}, nil
	}
	var out []any
	for {
		inner = strings.TrimSpace(inner)
		if inner == "" {
			return nil, fmt.Errorf("trailing comma in array")
		}
		if inner[0] == '"' {
			str, rest, err := parseString(inner)
			if err != nil {
				return nil, err
			}
			out = append(out, str)
			inner = strings.TrimSpace(rest)
		} else {
			end := strings.Index(inner, ",")
			var token string
			if end < 0 {
				token, inner = inner, ""
			} else {
				token, inner = strings.TrimSpace(inner[:end]), inner[end:]
			}
			n, err := strconv.ParseInt(token, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("unsupported array element %q", token)
			}
			out = append(out, n)
		}
		if inner == "" {
			return out, nil
		}
		if inner[0] != ',' {
			return nil, fmt.Errorf("expected comma between array elements, got %q", inner)
		}
		inner = inner[1:]
	}
}

// isBareKey reports whether s is a valid bare key or table name.
func isBareKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
