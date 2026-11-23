package i18n

import "testing"

func TestT(t *testing.T) {
	if got := T("fr-CA", "loan.overdue.title", "Dune"); got != "En retard : Dune" {
		t.Errorf("fr: %q", got)
	}
	if got := T("de", "loan.overdue.title", "Dune"); got != "Overdue: Dune" {
		t.Errorf("fallback: %q", got)
	}
	if got := T("en", "no.such.key"); got != "no.such.key" {
		t.Errorf("missing key: %q", got)
	}
}

func TestEveryEnglishKeyIsTranslated(t *testing.T) {
	for k := range messages["en"] {
		if _, ok := messages["fr"][k]; !ok {
			t.Errorf("fr is missing %q", k)
		}
	}
	for k := range messages["fr"] {
		if _, ok := messages["en"][k]; !ok {
			t.Errorf("en is missing %q", k)
		}
	}
}
