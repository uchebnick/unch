package semsearch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LocalTokensPath(localDir string) string {
	return filepath.Join(localDir, "tokens.json")
}

func GlobalTokensPath() (string, error) {
	configDir, err := globalConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "tokens.json"), nil
}

func ResolveProviderToken(localDir string, provider string) (string, error) {
	if envName := providerTokenEnv(provider); envName != "" {
		if token := strings.TrimSpace(os.Getenv(envName)); token != "" {
			return token, nil
		}
	}

	paths := make([]string, 0, 2)
	if globalPath, err := GlobalTokensPath(); err == nil {
		paths = append(paths, globalPath)
	} else {
		return "", err
	}
	if strings.TrimSpace(localDir) != "" {
		paths = append(paths, LocalTokensPath(localDir))
	}

	for _, path := range paths {
		tokens, err := loadTokensFile(path)
		if err != nil {
			return "", err
		}
		if token := strings.TrimSpace(tokens[provider]); token != "" {
			return token, nil
		}
	}

	globalPath, err := GlobalTokensPath()
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("missing token for provider %q; set %s or add %q to %s or %s", provider, providerTokenEnv(provider), provider, globalPath, LocalTokensPath(localDir))
}

func SaveProviderToken(path string, provider string, token string) error {
	path = strings.TrimSpace(path)
	provider = strings.TrimSpace(strings.ToLower(provider))
	token = strings.TrimSpace(token)

	if path == "" {
		return fmt.Errorf("token path is required")
	}
	if provider == "" {
		return fmt.Errorf("provider is required")
	}
	if token == "" {
		return fmt.Errorf("token is required")
	}

	tokens, err := loadTokensFile(path)
	if err != nil {
		return err
	}
	tokens[provider] = token

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func loadTokensFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var tokens map[string]string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if tokens == nil {
		return map[string]string{}, nil
	}
	return tokens, nil
}

func providerTokenEnv(provider string) string {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "openrouter":
		return "OPENROUTER_API_KEY"
	default:
		return ""
	}
}

func globalConfigDir() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("UNCH_CONFIG_HOME")); custom != "" {
		return filepath.Abs(custom)
	}

	configDir, err := os.UserConfigDir()
	if err == nil && strings.TrimSpace(configDir) != "" {
		return filepath.Join(configDir, "unch"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return "", fmt.Errorf("resolve global config dir: %w", err)
	}

	return filepath.Join(homeDir, ".config", "unch"), nil
}
