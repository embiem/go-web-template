package util

import (
	"strings"
	"testing"
)

func TestMdToHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{name: "heading", input: "# Hi", contains: "<h1"},
		{name: "paragraph", input: "hello world", contains: "<p"},
		{name: "emphasis", input: "*hi*", contains: "<em"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MdToHTML(tc.input)
			if !strings.Contains(got, tc.contains) {
				t.Errorf("MdToHTML(%q) = %q, want it to contain %q", tc.input, got, tc.contains)
			}
			if strings.Contains(got, "\n") {
				t.Errorf("MdToHTML(%q) = %q, want no newlines", tc.input, got)
			}
		})
	}
}
