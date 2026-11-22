package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Setenv("BIBLISANN_DATABASE_URL", "")
	tests := []struct {
		name string
		args []string
		code int
		out  string
	}{
		{"version", []string{"version"}, 0, ""},
		{"help", []string{"help"}, 0, "usage:"},
		{"unknown", []string{"frobnicate"}, 2, ""},
		{"serve without database fails", []string{"serve"}, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if got := run(context.Background(), tt.args, &out, &errb); got != tt.code {
				t.Fatalf("exit = %d, want %d (stderr %s)", got, tt.code, errb.String())
			}
			if !strings.Contains(out.String(), tt.out) {
				t.Errorf("stdout = %q", out.String())
			}
		})
	}
}

func TestOpenAPICommandOutputsValidJSON(t *testing.T) {
	var out bytes.Buffer
	if code := run(context.Background(), []string{"openapi"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatal("exit", code)
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || v["openapi"] == nil {
		t.Errorf("not an OpenAPI document: %v", err)
	}
}
