package format

import (
	"bytes"
	"strings"
	"testing"
)

type testItem struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	data := []testItem{{Name: "alice", Age: 30}}
	err := PrintJSON(&buf, data, false)
	if err != nil {
		t.Fatalf("PrintJSON error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "alice") {
		t.Errorf("output should contain 'alice', got: %s", output)
	}
	if !strings.Contains(output, "30") {
		t.Errorf("output should contain 30, got: %s", output)
	}
}

func TestPrintTable(t *testing.T) {
	var buf bytes.Buffer
	data := []testItem{{Name: "alice", Age: 30}, {Name: "bob", Age: 25}}
	err := PrintTable(&buf, data, false)
	if err != nil {
		t.Fatalf("PrintTable error: %v", err)
	}
}

func TestPrintMarkdown(t *testing.T) {
	var buf bytes.Buffer
	data := []testItem{{Name: "alice", Age: 30}, {Name: "bob", Age: 25}}
	err := PrintMarkdown(&buf, data)
	if err != nil {
		t.Fatalf("PrintMarkdown error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "| name |") {
		t.Errorf("Markdown should have header 'name', got: %s", output)
	}
	if !strings.Contains(output, "| alice |") {
		t.Errorf("Markdown should have data row, got: %s", output)
	}
	if !strings.Contains(output, "---") {
		t.Errorf("Markdown should have separator, got: %s", output)
	}
}

func TestPrintMarkdown_EmptySlice(t *testing.T) {
	var buf bytes.Buffer
	data := []testItem{}
	err := PrintMarkdown(&buf, data)
	if err != nil {
		t.Fatalf("PrintMarkdown on empty should not error: %v", err)
	}
	if !strings.Contains(buf.String(), "empty") {
		t.Errorf("empty result should say 'empty', got: %s", buf.String())
	}
}

func TestPrintMarkdown_NonSlice(t *testing.T) {
	var buf bytes.Buffer
	err := PrintMarkdown(&buf, "not a slice")
	if err == nil {
		t.Fatal("PrintMarkdown on non-struct should error")
	}
}

func TestPrintYAML(t *testing.T) {
	var buf bytes.Buffer
	data := []testItem{{Name: "alice", Age: 30}}
	err := PrintYAML(&buf, data)
	if err != nil {
		t.Fatalf("PrintYAML error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "name: alice") {
		t.Errorf("YAML output should contain 'name: alice', got: %s", output)
	}
}

func TestPrint_AutoSelect(t *testing.T) {
	var buf bytes.Buffer
	data := []testItem{{Name: "alice", Age: 30}}

	err := Print(&buf, "json", data, false)
	if err != nil {
		t.Fatalf("Print json: %v", err)
	}
	if !strings.Contains(buf.String(), "alice") {
		t.Error("json output")
	}

	buf.Reset()
	err = Print(&buf, "md", data, false)
	if err != nil {
		t.Fatalf("Print md: %v", err)
	}
	if !strings.Contains(buf.String(), "| name |") {
		t.Error("md output")
	}
}
