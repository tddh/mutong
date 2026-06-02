package format

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Print auto-selects output format.
func Print(w io.Writer, format string, data any, color bool) error {
	switch format {
	case "json":
		return PrintJSON(w, data, color)
	case "yaml":
		return PrintYAML(w, data)
	case "md", "markdown":
		return PrintMarkdown(w, data)
	case "table", "":
		return PrintTable(w, data, color)
	default:
		return fmt.Errorf("unknown output format: %s (supported: json, table, md, yaml)", format)
	}
}

// PrintJSON outputs indented JSON.
func PrintJSON(w io.Writer, data any, color bool) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(data)
}

// PrintTable outputs aligned terminal table.
func PrintTable(w io.Writer, data any, color bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintln(tw, "(table format: use -o md for Agent-friendly markdown)")
	return PrintJSON(tw, data, color)
}

// PrintMarkdown outputs Markdown table using reflection for headers from json tags.
func PrintMarkdown(w io.Writer, data any) error {
	v := reflect.ValueOf(data)
	if v.Kind() != reflect.Slice {
		// Wrap single struct/pointer in a slice for uniform table rendering
		slice := reflect.MakeSlice(reflect.SliceOf(v.Type()), 1, 1)
		slice.Index(0).Set(v)
		v = slice
	}
	if v.Len() == 0 {
		fmt.Fprintln(w, "(empty result)")
		return nil
	}

	elem := v.Index(0)
	t := elem.Type()
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("PrintMarkdown: unsupported element type %s, use -o json for non-struct types", t.Kind())
	}
	var headers []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := f.Tag.Get("json")
		if name == "" || name == "-" {
			name = f.Name
		}
		if idx := strings.Index(name, ","); idx >= 0 {
			name = name[:idx]
		}
		headers = append(headers, name)
	}

	// Header row
	fmt.Fprint(w, "|")
	for _, h := range headers {
		fmt.Fprintf(w, " %s |", h)
	}
	fmt.Fprintln(w)

	// Separator
	fmt.Fprint(w, "|")
	for range headers {
		fmt.Fprint(w, " --- |")
	}
	fmt.Fprintln(w)

	// Data rows
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i)
		if elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}
		fmt.Fprint(w, "|")
		for j := 0; j < elem.NumField(); j++ {
			val := elem.Field(j).Interface()
			s := formatFieldValue(val)
			fmt.Fprintf(w, " %s |", s)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func formatFieldValue(v any) string {
	if v == nil {
		return ""
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return ""
		}
		v = rv.Elem().Interface()
	}
	return fmt.Sprint(v)
}

// PrintYAML outputs YAML.
func PrintYAML(w io.Writer, data any) error {
	encoder := yaml.NewEncoder(w)
	encoder.SetIndent(2)
	return encoder.Encode(data)
}
