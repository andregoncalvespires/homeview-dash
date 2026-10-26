// Package yamlcfg reads and writes the small YAML subset HomeView uses to
// describe groups and services. It has no dependencies: block mappings,
// block sequences, comments and plain, 'single' or "double" quoted strings.
package yamlcfg

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// node is a parsed value: string, []any, map[string]any or nil.
type line struct {
	no     int
	indent int
	text   string
}

type parser struct {
	lines []line
	i     int
}

// Error is a syntax or schema problem tied to a line (0 = whole document).
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

func errAt(l int, f string, a ...any) *Error { return &Error{Line: l, Msg: fmt.Sprintf(f, a...)} }

// stripComment removes a trailing " # comment" that is outside quotes.
func stripComment(s string) string {
	var q rune
	for i, r := range s {
		switch {
		case q != 0:
			if r == q && !(q == '"' && i > 0 && s[i-1] == '\\') {
				q = 0
			}
		case r == '"' || r == '\'':
			// a quote only opens a string at the start of a scalar
			prev := strings.TrimRight(s[:i], " ")
			if prev == "" || strings.HasSuffix(prev, ":") || strings.HasSuffix(prev, "-") {
				q = r
			}
		case r == '#' && (i == 0 || s[i-1] == ' '):
			return s[:i]
		}
	}
	return s
}

func tokenize(src string) ([]line, error) {
	if !utf8.ValidString(src) {
		return nil, errAt(0, "not valid UTF-8")
	}
	src = strings.TrimPrefix(src, "\xef\xbb\xbf")
	var out []line
	for n, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		raw = strings.TrimRight(stripComment(raw), " \r")
		if strings.TrimSpace(raw) == "" {
			continue
		}
		ind := len(raw) - len(strings.TrimLeft(raw, " "))
		if strings.HasPrefix(raw[ind:], "\t") {
			return nil, errAt(n+1, "tabs are not allowed for indentation, use spaces")
		}
		t := raw[ind:]
		if ind == 0 && (t == "---" || t == "...") {
			continue
		}
		out = append(out, line{no: n + 1, indent: ind, text: t})
	}
	return out, nil
}

// Parse turns YAML text into nested map[string]any / []any / string / nil.
func parse(src string) (any, error) {
	ls, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	if len(ls) == 0 {
		return nil, nil
	}
	p := &parser{lines: ls}
	v, err := p.node(ls[0].indent, 0)
	if err != nil {
		return nil, err
	}
	if p.i < len(p.lines) {
		return nil, errAt(p.lines[p.i].no, "unexpected indentation")
	}
	return v, nil
}

func isSeq(t string) bool { return t == "-" || strings.HasPrefix(t, "- ") }

func (p *parser) node(indent, depth int) (any, error) {
	if depth > 12 {
		return nil, errAt(p.lines[p.i].no, "nested too deeply")
	}
	if isSeq(p.lines[p.i].text) {
		return p.seq(indent, depth)
	}
	return p.mapping(indent, depth)
}

func (p *parser) seq(indent, depth int) (any, error) {
	out := []any{}
	for p.i < len(p.lines) && p.lines[p.i].indent == indent && isSeq(p.lines[p.i].text) {
		l := p.lines[p.i]
		rest := strings.TrimLeft(l.text[1:], " ")
		if rest == "" {
			p.i++
			if p.i < len(p.lines) && p.lines[p.i].indent > indent {
				v, err := p.node(p.lines[p.i].indent, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			} else {
				out = append(out, nil)
			}
			continue
		}
		off := len(l.text) - len(rest)
		if _, _, ok := splitKey(rest); ok && !isSeq(rest) {
			// "- key: value": treat the rest as the first line of a mapping
			p.lines[p.i] = line{no: l.no, indent: indent + off, text: rest}
			v, err := p.mapping(indent+off, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			continue
		}
		s, err := scalar(rest, l.no)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		p.i++
	}
	return out, nil
}

func (p *parser) mapping(indent, depth int) (any, error) {
	out := map[string]any{}
	for p.i < len(p.lines) && p.lines[p.i].indent == indent && !isSeq(p.lines[p.i].text) {
		l := p.lines[p.i]
		k, v, ok := splitKey(l.text)
		if !ok {
			return nil, errAt(l.no, "expected \"key: value\"")
		}
		if _, dup := out[k]; dup {
			return nil, errAt(l.no, "duplicate key %q", k)
		}
		p.i++
		if v != "" {
			s, err := scalar(v, l.no)
			if err != nil {
				return nil, err
			}
			out[k] = s
			continue
		}
		switch {
		case p.i < len(p.lines) && p.lines[p.i].indent > indent:
			c, err := p.node(p.lines[p.i].indent, depth+1)
			if err != nil {
				return nil, err
			}
			out[k] = c
		case p.i < len(p.lines) && p.lines[p.i].indent == indent && isSeq(p.lines[p.i].text):
			c, err := p.seq(indent, depth+1)
			if err != nil {
				return nil, err
			}
			out[k] = c
		default:
			out[k] = nil
		}
	}
	return out, nil
}

// splitKey splits "key: value" (value may be empty). Keys are plain words.
func splitKey(t string) (k, v string, ok bool) {
	i := strings.Index(t, ":")
	for i >= 0 {
		if i+1 == len(t) || t[i+1] == ' ' {
			break
		}
		j := strings.Index(t[i+1:], ":")
		if j < 0 {
			return "", "", false
		}
		i += 1 + j
	}
	if i <= 0 {
		return "", "", false
	}
	k = strings.TrimSpace(t[:i])
	if k == "" || strings.ContainsAny(k, "\"'[]{}#&*!|>%@`") {
		return "", "", false
	}
	return k, strings.TrimSpace(t[i+1:]), true
}

func scalar(s string, no int) (any, error) {
	switch {
	case s == "[]" || s == "{}":
		return nil, nil
	case strings.HasPrefix(s, "\""):
		return unquoteDouble(s, no)
	case strings.HasPrefix(s, "'"):
		if len(s) < 2 || !strings.HasSuffix(s, "'") {
			return nil, errAt(no, "unterminated quoted string")
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	case s == "~" || s == "null":
		return "", nil
	case strings.ContainsAny(s[:1], "[]{}&*!|>%@`"):
		return nil, errAt(no, "unsupported YAML syntax (use plain or quoted text)")
	}
	return s, nil
}

func unquoteDouble(s string, no int) (any, error) {
	var b strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		switch {
		case c == '"':
			if strings.TrimSpace(s[i+1:]) != "" {
				return nil, errAt(no, "unexpected text after quoted string")
			}
			return b.String(), nil
		case c == '\\':
			i++
			if i >= len(s) {
				return nil, errAt(no, "unterminated quoted string")
			}
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '"', '\\', '/':
				b.WriteByte(s[i])
			case 'u':
				if i+4 >= len(s) {
					return nil, errAt(no, "bad \\u escape")
				}
				n, err := strconv.ParseUint(s[i+1:i+5], 16, 32)
				if err != nil {
					return nil, errAt(no, "bad \\u escape")
				}
				b.WriteRune(rune(n))
				i += 4
			default:
				return nil, errAt(no, "unknown escape \\%c", s[i])
			}
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return nil, errAt(no, "unterminated quoted string")
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
