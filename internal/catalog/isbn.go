package catalog

import (
	"errors"
	"strings"
)

// ErrInvalidISBN is returned for anything that is not a valid ISBN-10 or ISBN-13.
var ErrInvalidISBN = errors.New("invalid ISBN")

// NormalizeISBN accepts ISBN-10 or ISBN-13 with optional hyphens, spaces and an "ISBN" prefix and
// returns the canonical ISBN-13 and, when the book has a 978 prefix, the ISBN-10.
func NormalizeISBN(raw string) (isbn13, isbn10 string, err error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "ISBN-13")
	s = strings.TrimPrefix(s, "ISBN-10")
	s = strings.TrimPrefix(s, "ISBN")
	s = strings.TrimPrefix(strings.TrimSpace(s), ":")
	s = strings.NewReplacer("-", "", " ", "").Replace(s)

	switch len(s) {
	case 10:
		if !validISBN10(s) {
			return "", "", ErrInvalidISBN
		}
		isbn13 = "978" + s[:9]
		isbn13 += string(rune('0' + checkDigit13(isbn13)))
		return isbn13, s, nil
	case 13:
		if !allDigits(s) || (!strings.HasPrefix(s, "978") && !strings.HasPrefix(s, "979")) {
			return "", "", ErrInvalidISBN
		}
		if int(s[12]-'0') != checkDigit13(s[:12]) {
			return "", "", ErrInvalidISBN
		}
		if strings.HasPrefix(s, "978") {
			isbn10 = s[3:12]
			isbn10 += string(check10(isbn10))
		}
		return s, isbn10, nil
	}
	return "", "", ErrInvalidISBN
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func checkDigit13(first12 string) int {
	sum := 0
	for i, r := range first12 {
		d := int(r - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return (10 - sum%10) % 10
}

func validISBN10(s string) bool {
	if !allDigits(s[:9]) {
		return false
	}
	last := s[9]
	if last != 'X' && (last < '0' || last > '9') {
		return false
	}
	return check10(s[:9]) == last
}

func check10(first9 string) byte {
	sum := 0
	for i, r := range first9 {
		sum += (10 - i) * int(r-'0')
	}
	c := (11 - sum%11) % 11
	if c == 10 {
		return 'X'
	}
	return byte('0' + c)
}
