package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"testing"
)

func TestExtractTarGzRejectsEscapingSymlink(t *testing.T) {
	if stdruntime.GOOS == "windows" {
		t.Skip("symlink extraction tests are unreliable on Windows CI")
	}

	archivePath := filepath.Join(t.TempDir(), "payload.tar.gz")
	writeTarGz(t, archivePath, tarEntry{
		name:     "bundle/libllama.so",
		linkname: "../../escape",
		typ:      tar.TypeSymlink,
		mode:     0o777,
	})

	err := extractTarGz(archivePath, t.TempDir())
	if err == nil {
		t.Fatalf("expected escaping symlink archive to fail")
	}
	if !strings.Contains(err.Error(), "escapes destination") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractTarGzRejectsOverwriteThroughSymlink(t *testing.T) {
	if stdruntime.GOOS == "windows" {
		t.Skip("symlink extraction tests are unreliable on Windows CI")
	}

	archivePath := filepath.Join(t.TempDir(), "payload.tar.gz")
	writeTarGz(t, archivePath,
		tarEntry{name: "bundle/lib", typ: tar.TypeDir, mode: 0o755},
		tarEntry{name: "bundle/lib/llama.so", linkname: "../llama-real.so", typ: tar.TypeSymlink, mode: 0o777},
		tarEntry{name: "bundle/lib/llama.so", body: []byte("oops"), typ: tar.TypeReg, mode: 0o644},
	)

	err := extractTarGz(archivePath, t.TempDir())
	if err == nil {
		t.Fatalf("expected archive to fail when a regular file reuses a symlink path")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type tarEntry struct {
	name     string
	body     []byte
	linkname string
	typ      byte
	mode     int64
}

func writeTarGz(t *testing.T, archivePath string, entries ...tarEntry) {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Typeflag: entry.typ,
			Mode:     entry.mode,
			Linkname: entry.linkname,
			Size:     int64(len(entry.body)),
		}
		if entry.typ == tar.TypeDir {
			header.Size = 0
		}
		if entry.typ == 0 {
			header.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header %s: %v", entry.name, err)
		}
		if len(entry.body) > 0 {
			if _, err := tw.Write(entry.body); err != nil {
				t.Fatalf("write tar body %s: %v", entry.name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive file: %v", err)
	}
}
