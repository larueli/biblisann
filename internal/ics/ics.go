// Package ics renders an iCalendar (RFC 5545) feed of loan due dates.
package ics

import (
	"fmt"
	"strings"
	"time"
)

// Event is an all-day event.
type Event struct {
	UID     string
	Day     time.Time
	Summary string
	Stamp   time.Time
}

// Render returns the calendar with CRLF line endings and 75-octet line folding.
func Render(name string, events []Event) string {
	var b strings.Builder
	line := func(s string) {
		limit := 75
		for len(s) > limit {
			cut := limit
			for cut > 0 && s[cut]&0xC0 == 0x80 { // never split a UTF-8 sequence
				cut--
			}
			b.WriteString(s[:cut] + "\r\n ")
			s = s[cut:]
			limit = 74 // continuation lines start with a space
		}
		b.WriteString(s + "\r\n")
	}
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Biblisann//Loans//EN")
	line("CALSCALE:GREGORIAN")
	line("X-WR-CALNAME:" + escape(name))
	for _, e := range events {
		line("BEGIN:VEVENT")
		line("UID:" + escape(e.UID))
		line("DTSTAMP:" + e.Stamp.UTC().Format("20060102T150405Z"))
		line("DTSTART;VALUE=DATE:" + e.Day.Format("20060102"))
		line("DTEND;VALUE=DATE:" + e.Day.AddDate(0, 0, 1).Format("20060102"))
		line("SUMMARY:" + escape(e.Summary))
		line("TRANSP:TRANSPARENT")
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return b.String()
}

// escape applies RFC 5545 TEXT escaping and drops control characters, so user text can never
// start a new property line.
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == ';':
			b.WriteString(`\;`)
		case r == ',':
			b.WriteString(`\,`)
		case r == '\n' || r == '\r':
			b.WriteString(`\n`)
		case r < 0x20 || r == 0x7f:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// UIDFor builds a stable UID for a loan.
func UIDFor(id string) string { return fmt.Sprintf("loan-%s@biblisann", id) }
