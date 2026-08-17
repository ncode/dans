// Package delegation implements immutable RRset selector semantics.
package delegation

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ncode/dans/internal/dnsname"
)

// SelectorKind identifies how a selector interprets its pattern.
type SelectorKind string

const (
	SelectorExact SelectorKind = "exact"
	SelectorGlob  SelectorKind = "glob"
)

// ErrInvalidSelector reports a malformed or out-of-zone selector.
var ErrInvalidSelector = errors.New("delegation: invalid selector")

type tokenKind uint8

const (
	tokenLiteral tokenKind = iota
	tokenStar
	tokenQuestion
)

type patternToken struct {
	kind    tokenKind
	literal string
}

// Selector is a validated canonical exact or glob selector.
type Selector struct {
	zone    dnsname.Name
	kind    SelectorKind
	pattern string
	sqlLike string
	tokens  []patternToken
}

// CompileSelector validates and canonicalizes one selector for zone.
func CompileSelector(kind SelectorKind, pattern string, zone dnsname.Name) (Selector, error) {
	switch kind {
	case SelectorExact:
		name, err := dnsname.Parse(pattern)
		if err != nil || !name.Within(zone) {
			return Selector{}, ErrInvalidSelector
		}
		canonical := name.String()
		return Selector{
			zone:    zone,
			kind:    kind,
			pattern: canonical,
			sqlLike: escapeSQLLike(canonical),
			tokens:  []patternToken{{kind: tokenLiteral, literal: canonical}},
		}, nil
	case SelectorGlob:
		canonical, tokens, err := compileGlob(pattern)
		if err != nil || !patternWithin(canonical, zone.String()) {
			return Selector{}, ErrInvalidSelector
		}
		return Selector{
			zone:    zone,
			kind:    kind,
			pattern: canonical,
			sqlLike: tokensToSQLLike(tokens),
			tokens:  tokens,
		}, nil
	default:
		return Selector{}, ErrInvalidSelector
	}
}

// Kind returns the selector's immutable kind.
func (s Selector) Kind() SelectorKind { return s.kind }

// Pattern returns the canonical absolute selector pattern.
func (s Selector) Pattern() string { return s.pattern }

// SQLLike returns the creation-time SQL LIKE pattern using backslash as ESCAPE.
func (s Selector) SQLLike() string { return s.sqlLike }

// Matches reports whether the complete canonical owner name is selected.
func (s Selector) Matches(owner dnsname.Name) bool {
	if !owner.Within(s.zone) {
		return false
	}
	if s.kind == SelectorExact {
		return owner.String() == s.pattern
	}
	if owner.String() == s.zone.String() || owner.HasLiteralWildcard() {
		return false
	}
	return matchTokens(s.tokens, owner.String())
}

type patternLabel struct {
	raw     string
	hasMeta bool
}

func compileGlob(input string) (string, []patternToken, error) {
	labels, err := splitPattern(input)
	if err != nil {
		return "", nil, err
	}
	if len(labels) == 0 {
		return ".", []patternToken{{kind: tokenLiteral, literal: "."}}, nil
	}

	var pattern strings.Builder
	var tokens []patternToken
	wireLength := 1
	for _, label := range labels {
		var canonical string
		var labelTokens []patternToken
		var size int
		if label.hasMeta {
			canonical, labelTokens, size, err = compileMetaLabel(label.raw)
		} else {
			var name dnsname.Name
			name, err = dnsname.Parse(label.raw + ".")
			if err == nil {
				canonical = strings.TrimSuffix(name.String(), ".")
				size = canonicalWireLength(canonical)
				labelTokens = []patternToken{{kind: tokenLiteral, literal: canonical}}
			}
		}
		if err != nil || size > 63 {
			return "", nil, ErrInvalidSelector
		}
		wireLength += 1 + size
		if wireLength > 255 {
			return "", nil, ErrInvalidSelector
		}
		pattern.WriteString(canonical)
		pattern.WriteByte('.')
		tokens = append(tokens, labelTokens...)
		tokens = appendLiteral(tokens, ".")
	}
	return pattern.String(), tokens, nil
}

func splitPattern(input string) ([]patternLabel, error) {
	if input == "" {
		return nil, ErrInvalidSelector
	}
	var labels []patternLabel
	start := 0
	hasMeta := false
	for i := 0; i < len(input); {
		if input[i] == '\\' {
			if i+1 == len(input) {
				return nil, ErrInvalidSelector
			}
			if i+3 < len(input) && digit(input[i+1]) && digit(input[i+2]) && digit(input[i+3]) {
				value, _ := strconv.Atoi(input[i+1 : i+4])
				if value > 255 {
					return nil, ErrInvalidSelector
				}
				if value == '*' || value == '?' {
					hasMeta = true
				}
				i += 4
				continue
			}
			_, size := utf8.DecodeRuneInString(input[i+1:])
			if size == 0 || (size == 1 && input[i+1] >= utf8.RuneSelf) {
				return nil, ErrInvalidSelector
			}
			if input[i+1] == '*' || input[i+1] == '?' {
				hasMeta = true
			}
			i += 1 + size
			continue
		}

		r, size := utf8.DecodeRuneInString(input[i:])
		if r == utf8.RuneError && size == 1 {
			return nil, ErrInvalidSelector
		}
		if r == '*' || r == '?' {
			hasMeta = true
			i += size
			continue
		}
		if patternDot(r) {
			if i == start {
				if i+size == len(input) && len(labels) == 0 {
					return nil, nil
				}
				return nil, ErrInvalidSelector
			}
			labels = append(labels, patternLabel{raw: input[start:i], hasMeta: hasMeta})
			start = i + size
			hasMeta = false
			if start == len(input) {
				return labels, nil
			}
			i += size
			continue
		}
		i += size
	}
	return nil, ErrInvalidSelector
}

func compileMetaLabel(raw string) (string, []patternToken, int, error) {
	var pattern strings.Builder
	var tokens []patternToken
	literal := make([]byte, 0, len(raw))
	wireLength := 0
	flush := func() {
		if len(literal) == 0 {
			return
		}
		canonical := canonicalLiteral(literal)
		pattern.WriteString(canonical)
		tokens = appendLiteral(tokens, canonical)
		wireLength += len(literal)
		literal = literal[:0]
	}

	for i := 0; i < len(raw); {
		switch raw[i] {
		case '*', '?':
			flush()
			kind := tokenStar
			if raw[i] == '?' {
				kind = tokenQuestion
			}
			tokens = append(tokens, patternToken{kind: kind})
			pattern.WriteByte(raw[i])
			wireLength++
			i++
		case '\\':
			if i+3 < len(raw) && digit(raw[i+1]) && digit(raw[i+2]) && digit(raw[i+3]) {
				value, _ := strconv.Atoi(raw[i+1 : i+4])
				if value > 255 {
					return "", nil, 0, ErrInvalidSelector
				}
				literal = append(literal, byte(value))
				i += 4
				continue
			}
			r, size := utf8.DecodeRuneInString(raw[i+1:])
			if size == 0 || (size == 1 && raw[i+1] >= utf8.RuneSelf) || r >= utf8.RuneSelf {
				return "", nil, 0, ErrInvalidSelector
			}
			literal = append(literal, raw[i+1:i+1+size]...)
			i += 1 + size
		default:
			r, size := utf8.DecodeRuneInString(raw[i:])
			if r == utf8.RuneError && size == 1 || r >= utf8.RuneSelf {
				return "", nil, 0, ErrInvalidSelector
			}
			literal = append(literal, raw[i:i+size]...)
			i += size
		}
	}
	flush()
	return pattern.String(), tokens, wireLength, nil
}

func canonicalLiteral(value []byte) string {
	var text strings.Builder
	for _, c := range value {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 0x21 && c <= 0x7e && c != '.' && c != '\\' && c != '*' && c != '?' {
			text.WriteByte(c)
			continue
		}
		text.WriteByte('\\')
		text.WriteByte('0' + c/100)
		text.WriteByte('0' + c/10%10)
		text.WriteByte('0' + c%10)
	}
	return text.String()
}

func canonicalWireLength(value string) int {
	size := 0
	for i := 0; i < len(value); {
		if value[i] == '\\' && i+3 < len(value) && digit(value[i+1]) && digit(value[i+2]) && digit(value[i+3]) {
			size++
			i += 4
			continue
		}
		size++
		i++
	}
	return size
}

func appendLiteral(tokens []patternToken, value string) []patternToken {
	if value == "" {
		return tokens
	}
	if len(tokens) > 0 && tokens[len(tokens)-1].kind == tokenLiteral {
		tokens[len(tokens)-1].literal += value
		return tokens
	}
	return append(tokens, patternToken{kind: tokenLiteral, literal: value})
}

func patternWithin(pattern, zone string) bool {
	if zone == "." {
		return true
	}
	return pattern == zone || strings.HasSuffix(pattern, "."+zone)
}

func tokensToSQLLike(tokens []patternToken) string {
	var pattern strings.Builder
	for _, token := range tokens {
		switch token.kind {
		case tokenStar:
			pattern.WriteByte('%')
		case tokenQuestion:
			pattern.WriteByte('_')
		case tokenLiteral:
			pattern.WriteString(escapeSQLLike(token.literal))
		}
	}
	return pattern.String()
}

func escapeSQLLike(value string) string {
	var escaped strings.Builder
	for _, c := range value {
		if c == '\\' || c == '%' || c == '_' {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(c)
	}
	return escaped.String()
}

func matchTokens(tokens []patternToken, value string) bool {
	type state struct{ token, value int }
	seen := make(map[state]bool)
	var match func(int, int) bool
	match = func(tokenIndex, valueIndex int) bool {
		current := state{token: tokenIndex, value: valueIndex}
		if seen[current] {
			return false
		}
		seen[current] = true
		if tokenIndex == len(tokens) {
			return valueIndex == len(value)
		}
		token := tokens[tokenIndex]
		switch token.kind {
		case tokenLiteral:
			return strings.HasPrefix(value[valueIndex:], token.literal) && match(tokenIndex+1, valueIndex+len(token.literal))
		case tokenQuestion:
			return valueIndex < len(value) && match(tokenIndex+1, valueIndex+1)
		case tokenStar:
			return match(tokenIndex+1, valueIndex) || valueIndex < len(value) && match(tokenIndex, valueIndex+1)
		default:
			return false
		}
	}
	return match(0, 0)
}

func digit(c byte) bool { return c >= '0' && c <= '9' }

func patternDot(r rune) bool {
	return r == '.' || r == '\u3002' || r == '\uff0e' || r == '\uff61'
}
