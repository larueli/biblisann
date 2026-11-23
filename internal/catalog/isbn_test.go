package catalog

import "testing"

func TestNormalizeISBN(t *testing.T) {
	tests := []struct {
		in, want13, want10 string
		bad                bool
	}{
		{"9780306406157", "9780306406157", "0306406152", false},
		{"978-0-306-40615-7", "9780306406157", "0306406152", false},
		{"ISBN 0-306-40615-2", "9780306406157", "0306406152", false},
		{"ISBN-13: 978-2-07-036822-8", "9782070368228", "207036822X", false},
		{"080442957X", "9780804429573", "080442957X", false},
		{"080442957x", "9780804429573", "080442957X", false},
		{"9791032703687", "9791032703687", "", false}, // 979: no ISBN-10 form
		{"9780306406158", "", "", true},               // bad check digit
		{"0306406153", "", "", true},
		{"123", "", "", true},
		{"", "", "", true},
		{"3166480002407", "", "", true}, // EAN-13, not an ISBN
		{"97803064061AB", "", "", true},
		{"0306X06152", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			g13, g10, err := NormalizeISBN(tt.in)
			if (err != nil) != tt.bad {
				t.Fatalf("err = %v, bad = %v", err, tt.bad)
			}
			if g13 != tt.want13 || g10 != tt.want10 {
				t.Errorf("got %q %q, want %q %q", g13, g10, tt.want13, tt.want10)
			}
		})
	}
}
