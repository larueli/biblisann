package api

import (
	"encoding/json"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Date is a calendar date (YYYY-MM-DD) without time zone, as used for due dates.
type Date struct{ time.Time }

// Schema implements huma.SchemaProvider.
func (Date) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "date", Examples: []any{"2026-12-24"}}
}

// UnmarshalJSON accepts "YYYY-MM-DD".
func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return err
	}
	d.Time = t
	return nil
}

// MarshalJSON writes "YYYY-MM-DD".
func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.Format("2006-01-02")) }

// toTime converts an optional Date to an optional time.
func toTime(d *Date) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time
	return &t
}

// toDate converts an optional time to an optional Date.
func toDate(t *time.Time) *Date {
	if t == nil {
		return nil
	}
	return &Date{*t}
}
