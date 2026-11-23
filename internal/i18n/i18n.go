// Package i18n holds the server-side message catalog (notifications, calendar entries). The web
// app has its own catalog; only text the server composes lives here.
package i18n

import (
	"fmt"
	"strings"
)

// Messages by language then key. English is the source language and the fallback.
var messages = map[string]map[string]string{
	"en": {
		"loan.due_soon.title":  "Due tomorrow: %s",
		"loan.due_today.title": "Due today: %s",
		"loan.overdue.title":   "Overdue: %s",
		"loan.lent.body":       "“%s” lent to %s is due on %s.",
		"loan.borrowed.body":   "“%s” borrowed from %s is due on %s.",
		"ics.lent":             "Get back: %s (from %s)",
		"ics.borrowed":         "Return: %s (to %s)",
		"test.title":           "Biblisann test notification",
		"test.body":            "If you can read this, notifications work.",
	},
	"fr": {
		"loan.due_soon.title":  "À rendre demain : %s",
		"loan.due_today.title": "À rendre aujourd'hui : %s",
		"loan.overdue.title":   "En retard : %s",
		"loan.lent.body":       "« %s » prêté à %s est attendu le %s.",
		"loan.borrowed.body":   "« %s » emprunté à %s est à rendre le %s.",
		"ics.lent":             "Récupérer : %s (chez %s)",
		"ics.borrowed":         "Rendre : %s (à %s)",
		"test.title":           "Notification de test Biblisann",
		"test.body":            "Si vous lisez ceci, les notifications fonctionnent.",
	},
}

// T formats the message key in the given language (a BCP 47 tag such as "fr-CA"), falling back
// to English, then to the key itself.
func T(lang, key string, args ...any) string {
	lang = strings.ToLower(lang)
	if i := strings.IndexAny(lang, "-_"); i > 0 {
		lang = lang[:i]
	}
	msg, ok := messages[lang][key]
	if !ok {
		if msg, ok = messages["en"][key]; !ok {
			return key
		}
	}
	return fmt.Sprintf(msg, args...)
}
