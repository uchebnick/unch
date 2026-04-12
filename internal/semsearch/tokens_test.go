package semsearch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProviderTokenPrefersEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "env-token")
	t.Setenv("SEMSEARCH_HOME", t.TempDir())

	root := t.TempDir()
	token, err := ResolveProviderToken(filepath.Join(root, ".semsearch"), "openrouter")
	if err != nil {
		t.Fatalf("ResolveProviderToken() error: %v", err)
	}
	if token != "env-token" {
		t.Fatalf("ResolveProviderToken() = %q", token)
	}
}

func TestResolveProviderTokenFallsBackToLocalFile(t *testing.T) {
	t.Setenv("SEMSEARCH_HOME", t.TempDir())

	localDir := filepath.Join(t.TempDir(), ".semsearch")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(LocalTokensPath(localDir), []byte("{\"openrouter\":\"local-token\"}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	token, err := ResolveProviderToken(localDir, "openrouter")
	if err != nil {
		t.Fatalf("ResolveProviderToken() error: %v", err)
	}
	if token != "local-token" {
		t.Fatalf("ResolveProviderToken() = %q", token)
	}
}

func TestResolveProviderTokenFallsBackToGlobalFile(t *testing.T) {
	semsearchHome := t.TempDir()
	t.Setenv("SEMSEARCH_HOME", semsearchHome)

	globalPath, err := GlobalTokensPath()
	if err != nil {
		t.Fatalf("GlobalTokensPath() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(globalPath, []byte("{\"openrouter\":\"global-token\"}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	token, err := ResolveProviderToken(filepath.Join(t.TempDir(), ".semsearch"), "openrouter")
	if err != nil {
		t.Fatalf("ResolveProviderToken() error: %v", err)
	}
	if token != "global-token" {
		t.Fatalf("ResolveProviderToken() = %q", token)
	}
}

func TestSaveProviderTokenMergesExistingProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	if err := os.WriteFile(path, []byte("{\"other\":\"keep-me\"}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	if err := SaveProviderToken(path, "openrouter", "saved-token"); err != nil {
		t.Fatalf("SaveProviderToken() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "\"other\": \"keep-me\"") {
		t.Fatalf("tokens file = %q, want preserved provider", text)
	}
	if !strings.Contains(text, "\"openrouter\": \"saved-token\"") {
		t.Fatalf("tokens file = %q, want saved provider", text)
	}
}

func TestResolveProviderTokenPrefersGlobalBeforeLocalFile(t *testing.T) {
	semsearchHome := t.TempDir()
	t.Setenv("SEMSEARCH_HOME", semsearchHome)

	globalPath, err := GlobalTokensPath()
	if err != nil {
		t.Fatalf("GlobalTokensPath() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(globalPath, []byte("{\"openrouter\":\"global-token\"}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	localDir := filepath.Join(t.TempDir(), ".semsearch")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(LocalTokensPath(localDir), []byte("{\"openrouter\":\"local-token\"}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	token, err := ResolveProviderToken(localDir, "openrouter")
	if err != nil {
		t.Fatalf("ResolveProviderToken() error: %v", err)
	}
	if token != "global-token" {
		t.Fatalf("ResolveProviderToken() = %q", token)
	}
}
