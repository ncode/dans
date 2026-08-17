// Package dnsname canonicalizes absolute DNS owner names for policy decisions.
package dnsname

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

var (
	// ErrInvalidName reports a relative, malformed, or overlong DNS name.
	ErrInvalidName = errors.New("dns name: invalid absolute name")
	idnaProfile    = idna.New(
		idna.MapForLookup(),
		idna.Transitional(false),
		idna.StrictDomainName(false),
		idna.ValidateLabels(true),
		idna.VerifyDNSLength(false),
	)
)

// Name is a canonical absolute DNS name.
type Name struct {
	text   string
	labels []string
}

// Parse canonicalizes an absolute DNS presentation name.
func Parse(input string) (Name, error) {
	labels, err := parseLabels(input)
	if err != nil {
		return Name{}, err
	}

	canonical := make([]string, 0, len(labels))
	wireLength := 1 // root label
	for _, label := range labels {
		text, size, err := canonicalLabel(label)
		if err != nil {
			return Name{}, err
		}
		if size > 63 {
			return Name{}, fmt.Errorf("%w: label exceeds 63 octets", ErrInvalidName)
		}
		wireLength += 1 + size
		if wireLength > 255 {
			return Name{}, fmt.Errorf("%w: name exceeds 255 wire octets", ErrInvalidName)
		}
		canonical = append(canonical, text)
	}

	if len(canonical) == 0 {
		return Name{text: "."}, nil
	}
	return Name{text: strings.Join(canonical, ".") + ".", labels: canonical}, nil
}

func parseLabels(input string) ([][]byte, error) {
	if input == "" {
		return nil, ErrInvalidName
	}

	var labels [][]byte
	label := make([]byte, 0, len(input))
	absolute := false
	for i := 0; i < len(input); {
		if input[i] == '\\' {
			if i+1 == len(input) {
				return nil, fmt.Errorf("%w: dangling escape", ErrInvalidName)
			}
			if i+3 < len(input) && isDigit(input[i+1]) && isDigit(input[i+2]) && isDigit(input[i+3]) {
				value, _ := strconv.Atoi(input[i+1 : i+4])
				if value > 255 {
					return nil, fmt.Errorf("%w: decimal escape exceeds 255", ErrInvalidName)
				}
				label = append(label, byte(value))
				i += 4
				continue
			}
			_, size := utf8.DecodeRuneInString(input[i+1:])
			if size == 0 || (size == 1 && input[i+1] >= utf8.RuneSelf) {
				return nil, fmt.Errorf("%w: malformed escaped UTF-8", ErrInvalidName)
			}
			label = append(label, input[i+1:i+1+size]...)
			i += 1 + size
			continue
		}

		r, size := utf8.DecodeRuneInString(input[i:])
		if r == utf8.RuneError && size == 1 {
			return nil, fmt.Errorf("%w: malformed UTF-8", ErrInvalidName)
		}
		if isDot(r) {
			if len(label) == 0 {
				if i+size == len(input) && len(labels) == 0 {
					return nil, nil // root
				}
				return nil, fmt.Errorf("%w: empty label", ErrInvalidName)
			}
			labels = append(labels, label)
			label = nil
			absolute = i+size == len(input)
			i += size
			continue
		}
		if absolute {
			return nil, fmt.Errorf("%w: data after root label", ErrInvalidName)
		}
		label = append(label, input[i:i+size]...)
		i += size
	}
	if !absolute || len(label) != 0 {
		return nil, fmt.Errorf("%w: relative name", ErrInvalidName)
	}
	return labels, nil
}

func canonicalLabel(raw []byte) (string, int, error) {
	value := raw
	if utf8.Valid(raw) && (hasNonASCII(raw) || hasALabelPrefix(raw)) {
		ascii, err := idnaProfile.ToASCII(string(raw))
		if err != nil {
			return "", 0, fmt.Errorf("%w: IDNA label: %v", ErrInvalidName, err)
		}
		value = []byte(ascii)
	}

	value = append([]byte(nil), value...)
	for i, c := range value {
		if c >= 'A' && c <= 'Z' {
			value[i] = c + ('a' - 'A')
		}
	}

	var text strings.Builder
	text.Grow(len(value))
	for _, c := range value {
		if c >= 0x21 && c <= 0x7e && c != '.' && c != '\\' {
			text.WriteByte(c)
			continue
		}
		text.WriteByte('\\')
		text.WriteByte('0' + c/100)
		text.WriteByte('0' + c/10%10)
		text.WriteByte('0' + c%10)
	}
	return text.String(), len(value), nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isDot(r rune) bool {
	return r == '.' || r == '\u3002' || r == '\uff0e' || r == '\uff61'
}

func hasNonASCII(value []byte) bool {
	for _, c := range value {
		if c >= utf8.RuneSelf {
			return true
		}
	}
	return false
}

func hasALabelPrefix(value []byte) bool {
	return len(value) >= 4 && strings.EqualFold(string(value[:4]), "xn--")
}

// String returns the canonical absolute presentation name.
func (n Name) String() string { return n.text }

// Within reports whether n is the zone apex or one of its descendants.
func (n Name) Within(zone Name) bool {
	if len(zone.labels) > len(n.labels) {
		return false
	}
	offset := len(n.labels) - len(zone.labels)
	for i := range zone.labels {
		if n.labels[offset+i] != zone.labels[i] {
			return false
		}
	}
	return true
}

// HasLiteralWildcard reports whether any complete owner-name label is "*".
func (n Name) HasLiteralWildcard() bool {
	for _, label := range n.labels {
		if label == "*" {
			return true
		}
	}
	return false
}
