package embed

import (
	"fmt"
	"strings"
)

type Provider string

const (
	ProviderLlamaCPP  Provider = "llama.cpp"
	ProviderOpenRouter Provider = "openrouter"
)

func DefaultProvider() Provider {
	return ProviderLlamaCPP
}

func ParseProvider(value string) (Provider, error) {
	token := strings.ToLower(strings.TrimSpace(value))
	switch token {
	case "", string(ProviderLlamaCPP), "llama", "llamacpp":
		return ProviderLlamaCPP, nil
	case string(ProviderOpenRouter):
		return ProviderOpenRouter, nil
	default:
		return "", fmt.Errorf("unknown embedding provider %q; expected llama.cpp or openrouter", value)
	}
}

func (p Provider) String() string {
	return string(p)
}
