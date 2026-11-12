package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newHelperContext(t *testing.T, rawQuery string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
	return c
}

func TestQueryIntDefault(t *testing.T) {
	tests := []struct {
		query string
		def   int
		want  int
	}{
		{"page=7", 1, 7},
		{"page=0", 1, 0},
		{"page=-3", 1, -3},
		{"page=abc", 5, 5},
		{"page=", 5, 5},
		{"", 9, 9},
		{"page=12.5", 4, 4},
	}
	for _, tc := range tests {
		c := newHelperContext(t, tc.query)
		if got := QueryIntDefault(c, "page", tc.def); got != tc.want {
			t.Errorf("QueryIntDefault(%q, %d) = %d, want %d", tc.query, tc.def, got, tc.want)
		}
	}
}

func TestQueryInt64Default(t *testing.T) {
	tests := []struct {
		query string
		def   int64
		want  int64
	}{
		{"ts=1700000000000", 1, 1700000000000},
		{"ts=-5", 1, -5},
		{"ts=9999999999999999999999", 42, 42}, // overflow falls back
		{"ts=xyz", 42, 42},
		{"", 42, 42},
	}
	for _, tc := range tests {
		c := newHelperContext(t, tc.query)
		if got := QueryInt64Default(c, "ts", tc.def); got != tc.want {
			t.Errorf("QueryInt64Default(%q, %d) = %d, want %d", tc.query, tc.def, got, tc.want)
		}
	}
}

func TestExtractQueryParams(t *testing.T) {
	c := newHelperContext(t, "a=1&b=two&b=three&empty=")
	got := extractQueryParams(c)
	if got["a"] != "1" || got["b"] != "two" {
		t.Fatalf("first value must win for repeated keys: %v", got)
	}
	if v, ok := got["empty"]; !ok || v != "" {
		t.Fatalf("empty-valued key must be preserved: %v", got)
	}
	if len(got) != 3 {
		t.Fatalf("unexpected param set: %v", got)
	}
}

func TestErrorJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ErrorJSON(c, http.StatusTeapot, "boom")

	if w.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTeapot)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "boom" {
		t.Fatalf("error field = %q, want boom", body["error"])
	}
}
