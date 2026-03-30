package indexing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveGitignorePath(t *testing.T) {
	t.Parallel()

	root := "/tmp/project"

	got, err := ResolveGitignorePath(root)
	if err != nil || got != filepath.Join(root, ".gitignore") {
		t.Fatalf("ResolveGitignorePath(default) = (%q, %v)", got, err)
	}

	got, err = ResolveGitignorePath(root, "custom.ignore")
	if err != nil || got != filepath.Join(root, "custom.ignore") {
		t.Fatalf("ResolveGitignorePath(relative) = (%q, %v)", got, err)
	}

	if _, err := ResolveGitignorePath(root, "a", "b"); err == nil {
		t.Fatalf("expected error for too many gitignore paths")
	}
}

func TestExtractPrefixedBlocksAndReadContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	content := strings.Join([]string{
		"// @filectx: file context",
		"// @search: first comment",
		"func First() {}",
		"// regular comment",
		"// @search second comment",
		"func Second() {}",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write sample file: %v", err)
	}

	comments, ctx, err := ExtractPrefixedBlocks(path, "@search:", "@filectx:")
	if err != nil {
		t.Fatalf("ExtractPrefixedBlocks() error: %v", err)
	}
	if len(comments) != 3 {
		t.Fatalf("expected three indexed directives (including filectx), got %d", len(comments))
	}
	if ctx != "file context" {
		t.Fatalf("context = %q", ctx)
	}
	if comments[1].Text != "first comment" || comments[1].FollowingText == "" {
		t.Fatalf("unexpected comment payload: %+v", comments[1])
	}

	scanner := FileScanner{}
	text, readCtx, err := scanner.ReadSearchResultContent(path, 2, "@search:", "@filectx:")
	if err != nil {
		t.Fatalf("ReadSearchResultContent() error: %v", err)
	}
	if text != "first comment" || readCtx != "file context" {
		t.Fatalf("ReadSearchResultContent() = (%q, %q)", text, readCtx)
	}
}

func TestCollectJobsSkipsNoiseAndRespectsGitignore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.go\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".semsearch"), 0o755); err != nil {
		t.Fatalf("mkdir .semsearch: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	files := map[string]string{
		"keep.go":      "// @search: keep me\nfunc Keep() {}\n",
		"ignored.go":   "// @search: ignored by gitignore\n",
		"README.md":    "// @search: ignored readme\n",
		".semsearch/a": "// @search: ignored local state\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir parent for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	jobs, total, err := (FileScanner{}).CollectJobs(root, filepath.Join(root, ".gitignore"), nil, "@search:", "@filectx:")
	if err != nil {
		t.Fatalf("CollectJobs() error: %v", err)
	}
	if total != 1 || len(jobs) != 1 {
		t.Fatalf("CollectJobs() = jobs=%v total=%d", jobs, total)
	}
	if jobs[0].Path != filepath.Join(root, "keep.go") {
		t.Fatalf("unexpected job path %q", jobs[0].Path)
	}
}

func TestLooksLikeBinaryFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bin.dat")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open binary file: %v", err)
	}
	defer f.Close()

	binary, err := looksLikeBinaryFile(f)
	if err != nil {
		t.Fatalf("looksLikeBinaryFile() error: %v", err)
	}
	if !binary {
		t.Fatalf("expected file to be detected as binary")
	}
}
