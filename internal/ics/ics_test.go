package ics

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRender(t *testing.T) {
	day := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	out := Render("Loans", []Event{{UID: "u1", Day: day, Summary: "Return: Dune, vol. 1; to Marie", Stamp: day}})
	for _, want := range []string{"BEGIN:VCALENDAR\r\n", "DTSTART;VALUE=DATE:20261224\r\n", "DTEND;VALUE=DATE:20261225\r\n",
		`SUMMARY:Return: Dune\, vol. 1\; to Marie` + "\r\n", "END:VCALENDAR\r\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestInjectionIsEscaped(t *testing.T) {
	out := Render("x", []Event{{UID: "u", Day: time.Now(), Summary: "evil\r\nBEGIN:VEVENT\r\nATTENDEE:mailto:a@b", Stamp: time.Now()}})
	if strings.Count(out, "\r\nBEGIN:VEVENT") != 1 || strings.Contains(out, "\r\nATTENDEE") {
		t.Errorf("summary injected a property or event:\n%s", out)
	}
}

func TestFoldingNeverBreaksUTF8AndRespectsLimit(t *testing.T) {
	out := Render("x", []Event{{UID: "u", Day: time.Now(), Summary: strings.Repeat("é", 200), Stamp: time.Now()}})
	for _, l := range strings.Split(strings.TrimSuffix(out, "\r\n"), "\r\n") {
		if len(l) > 75 {
			t.Errorf("line of %d octets: %q", len(l), l)
		}
		if !utf8.ValidString(l) {
			t.Errorf("invalid UTF-8 in line %q", l)
		}
	}
	unfolded := strings.ReplaceAll(out, "\r\n ", "")
	if !strings.Contains(unfolded, "SUMMARY:"+strings.Repeat("é", 200)) {
		t.Error("unfolded summary differs from the original")
	}
}
