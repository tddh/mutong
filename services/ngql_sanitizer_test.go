package services

import (
	"testing"
)

func TestNGQLSanitizer_EscapeString(t *testing.T) {
	s := NewNGQLSanitizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"normal", "hello", "hello"},
		{"single_quote", "hello'world", "hello\\'world"},
		{"backslash", "hello\\world", "hello\\\\world"},
		{"combined", "hello'world\\test", "hello\\'world\\\\test"},
		{"newline", "hello\nworld", "hello\\nworld"},
		{"tab", "hello\tworld", "hello\\tworld"},
		{"carriage_return", "hello\rworld", "hello\\rworld"},
		{"control_chars", "hello\x00world", "helloworld"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.EscapeString(tt.input)
			if result != tt.expected {
				t.Errorf("EscapeString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNGQLSanitizer_QuoteString(t *testing.T) {
	s := NewNGQLSanitizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", "''"},
		{"normal", "hello", "'hello'"},
		{"with_quote", "hello'world", "'hello\\'world'"},
		{"injection_attempt", "abc'}) RETURN 1; DROP TAG K8sResource; //", "'abc\\'}) RETURN 1; DROP TAG K8sResource; //'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.QuoteString(tt.input)
			if result != tt.expected {
				t.Errorf("QuoteString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNGQLSanitizer_ValidateIdentifier(t *testing.T) {
	s := NewNGQLSanitizer()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid", "K8sResource", true},
		{"valid_underscore", "my_tag", true},
		{"valid_with_number", "tag123", true},
		{"empty", "", false},
		{"starts_with_number", "123tag", false},
		{"special_chars", "my-tag", false},
		{"reserved_word", "MATCH", false},
		{"reserved_word_lower", "match", false},
		{"too_long", string(make([]byte, 257)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.ValidateIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("ValidateIdentifier(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestWhitelistKind(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedKind string
		expectedOk   bool
	}{
		{"valid_pod", "Pod", "Pod", true},
		{"valid_node", "Node", "Node", true},
		{"valid_service", "Service", "Service", true},
		{"valid_deployment", "Deployment", "Deployment", true},
		{"invalid", "MaliciousKind", "", false},
		{"injection_attempt", "Pod'; DROP TAG--", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, ok := WhitelistKind(tt.input)
			if kind != tt.expectedKind || ok != tt.expectedOk {
				t.Errorf("WhitelistKind(%q) = (%q, %v), want (%q, %v)",
					tt.input, kind, ok, tt.expectedKind, tt.expectedOk)
			}
		})
	}
}
