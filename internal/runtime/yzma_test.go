package runtime

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func TestValidateAndDetectYzmaLibDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	required := requiredYzmaLibFiles()
	for _, name := range required {
		if err := os.WriteFile(filepath.Join(root, name), []byte("stub"), 0o644); err != nil {
			t.Fatalf("write required lib %s: %v", name, err)
		}
	}

	got, ok := validateYzmaLibDir(root)
	if !ok {
		t.Fatalf("validateYzmaLibDir() returned false for valid dir")
	}
	if got != filepath.Clean(root) {
		t.Fatalf("validateYzmaLibDir() = %q, want %q", got, filepath.Clean(root))
	}

	detected, ok := detectedYzmaLibDir(root)
	if !ok || detected != filepath.Clean(root) {
		t.Fatalf("detectedYzmaLibDir(root) = (%q, %v)", detected, ok)
	}

	wrapped := t.TempDir()
	libDir := filepath.Join(wrapped, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir lib dir: %v", err)
	}
	for _, name := range required {
		if err := os.WriteFile(filepath.Join(libDir, name), []byte("stub"), 0o644); err != nil {
			t.Fatalf("write nested required lib %s: %v", name, err)
		}
	}

	detected, ok = detectedYzmaLibDir(wrapped)
	if !ok || detected != filepath.Clean(libDir) {
		t.Fatalf("detectedYzmaLibDir(wrapper) = (%q, %v), want %q", detected, ok, filepath.Clean(libDir))
	}
}

func TestResolveYzmaLibPath(t *testing.T) {
	root := t.TempDir()
	for _, name := range requiredYzmaLibFiles() {
		if err := os.WriteFile(filepath.Join(root, name), []byte("stub"), 0o644); err != nil {
			t.Fatalf("write required lib %s: %v", name, err)
		}
	}

	got, note, err := ResolveYzmaLibPath(root)
	if err != nil {
		t.Fatalf("ResolveYzmaLibPath(valid dir) error: %v", err)
	}
	if got != filepath.Clean(root) || note != "" {
		t.Fatalf("ResolveYzmaLibPath(valid dir) = (%q, %q)", got, note)
	}

	libFile := filepath.Join(root, requiredYzmaLibFiles()[0])
	got, _, err = ResolveYzmaLibPath(libFile)
	if err != nil {
		t.Fatalf("ResolveYzmaLibPath(lib file) error: %v", err)
	}
	if got != filepath.Clean(root) {
		t.Fatalf("ResolveYzmaLibPath(lib file) = %q", got)
	}

	fallback := t.TempDir()
	for _, name := range requiredYzmaLibFiles() {
		if err := os.WriteFile(filepath.Join(fallback, name), []byte("stub"), 0o644); err != nil {
			t.Fatalf("write fallback lib %s: %v", name, err)
		}
	}
	t.Setenv("YZMA_LIB", fallback)

	got, note, err = ResolveYzmaLibPath(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("ResolveYzmaLibPath(fallback) error: %v", err)
	}
	if got != filepath.Clean(fallback) || !strings.Contains(note, "using YZMA_LIB=") {
		t.Fatalf("ResolveYzmaLibPath(fallback) = (%q, %q)", got, note)
	}
}

func TestDynamicLibraryHelpers(t *testing.T) {
	envVar := dynamicLibraryLookupEnvVar()
	if envVar == "" {
		t.Fatalf("dynamicLibraryLookupEnvVar() returned empty for GOOS=%s", goruntime.GOOS)
	}

	var libPath string
	switch goruntime.GOOS {
	case "windows":
		libPath = `C:\tmp\llama.dll`
	case "linux", "freebsd":
		libPath = "/tmp/libllama.so"
	default:
		libPath = "/tmp/libllama.dylib"
	}
	if !looksLikeDynamicLibraryPath(libPath) {
		t.Fatalf("looksLikeDynamicLibraryPath(%q) = false", libPath)
	}

	t.Setenv(envVar, "")
	dir := t.TempDir()
	if err := EnsureDynamicLibraryLookupPath(dir); err != nil {
		t.Fatalf("EnsureDynamicLibraryLookupPath(empty) error: %v", err)
	}
	if got := os.Getenv(envVar); got != dir {
		t.Fatalf("EnsureDynamicLibraryLookupPath(empty) = %q, want %q", got, dir)
	}

	if err := EnsureDynamicLibraryLookupPath(dir); err != nil {
		t.Fatalf("EnsureDynamicLibraryLookupPath(duplicate) error: %v", err)
	}
	if got := os.Getenv(envVar); got != dir {
		t.Fatalf("duplicate path should not be appended, got %q", got)
	}
}

func TestReplaceManagedDir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	src := filepath.Join(base, "src")
	dst := filepath.Join(base, "dst")

	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write dst file: %v", err)
	}

	if err := replaceManagedDir(src, dst); err != nil {
		t.Fatalf("replaceManagedDir() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "new.txt")); err != nil {
		t.Fatalf("new file missing after replace: %v", err)
	}
	if _, err := os.Stat(dst + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup dir should be removed, stat err = %v", err)
	}
}
