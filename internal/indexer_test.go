package internal

import "testing"

func TestShouldSkipIndexedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: ".", want: false},
		{path: "internal/cli.go", want: false},
		{path: ".git/config", want: true},
		{path: ".semsearch/index.db", want: true},
		{path: "README.md", want: true},
		{path: "docs/README.dev.md", want: true},
		{path: "guides/readme.txt", want: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := shouldSkipIndexedPath(tt.path); got != tt.want {
				t.Fatalf("shouldSkipIndexedPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestCollectFollowingLines(t *testing.T) {
	t.Parallel()

	lines := []string{
		"line1",
		"line2",
		"line3",
		"line4",
		"line5",
	}

	tests := []struct {
		name  string
		line  int
		limit int
		want  string
	}{
		{name: "middle window", line: 2, limit: 2, want: "line3\nline4"},
		{name: "clamped at eof", line: 4, limit: 10, want: "line5"},
		{name: "last line has no following text", line: 5, limit: 3, want: ""},
		{name: "invalid line", line: 0, limit: 3, want: ""},
		{name: "invalid limit", line: 2, limit: 0, want: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := collectFollowingLines(lines, tt.line, tt.limit); got != tt.want {
				t.Fatalf("collectFollowingLines(line=%d, limit=%d) = %q, want %q", tt.line, tt.limit, got, tt.want)
			}
		})
	}
}

func TestExtractDirectivePayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		line   string
		prefix string
		want   string
		ok     bool
	}{
		{name: "slash comment", line: "// @search: semantic search entrypoint", prefix: "@search:", want: "semantic search entrypoint", ok: true},
		{name: "block comment", line: "/* @search: block comment works */", prefix: "@search:", want: "block comment works", ok: true},
		{name: "star comment", line: "* @filectx: file level context", prefix: "@filectx:", want: "file level context", ok: true},
		{name: "hash comment", line: "# @search: shell style", prefix: "@search:", want: "shell style", ok: true},
		{name: "prefix without colon still matches", line: "// @search something", prefix: "@search:", want: "something", ok: true},
		{name: "non matching", line: "// regular comment", prefix: "@search:", want: "", ok: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := extractDirectivePayload(tt.line, tt.prefix)
			if ok != tt.ok {
				t.Fatalf("extractDirectivePayload(%q, %q) ok = %v, want %v", tt.line, tt.prefix, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("extractDirectivePayload(%q, %q) = %q, want %q", tt.line, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestLooksLikeBinary(t *testing.T) {
	t.Parallel()

	if looksLikeBinary([]byte("package main\nfunc main() {}\n")) {
		t.Fatalf("expected plain text not to be detected as binary")
	}
	if !looksLikeBinary([]byte{0x00, 0x01, 0x02, 0x03}) {
		t.Fatalf("expected data with NUL byte to be detected as binary")
	}
}
