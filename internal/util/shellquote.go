package util

import "strings"

// ShellQuote quotes a string for safe use in a POSIX shell command.
// It wraps the string in single quotes and escapes any embedded single quotes.
// This prevents shell injection attacks when building remote commands.
func ShellQuote(s string) string {
	// Empty string → ''
	if s == "" {
		return "''"
	}
	// If string contains no special characters, return as-is
	if !containsShellSpecial(s) {
		return s
	}
	// Replace ' with '\'' and wrap in single quotes
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// ShellQuoteArgs quotes each argument and joins them with spaces.
func ShellQuoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = ShellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

// containsShellSpecial returns true if the string contains characters
// that have special meaning in a POSIX shell.
func containsShellSpecial(s string) bool {
	for _, c := range s {
		switch c {
		case ' ', '\t', '\n', '\'', '"', '\\', '$', '`', '!',
			'&', '|', ';', '(', ')', '<', '>', '{', '}',
			'[', ']', '?', '*', '#', '~', '^':
			return true
		}
	}
	return false
}
