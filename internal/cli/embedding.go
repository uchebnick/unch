package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	appembed "github.com/uchebnick/unch/internal/embed"
	llamaembed "github.com/uchebnick/unch/internal/embed/llama"
	openrouterembed "github.com/uchebnick/unch/internal/embed/openrouter"
	"github.com/uchebnick/unch/internal/runtime"
	"github.com/uchebnick/unch/internal/semsearch"
	"github.com/uchebnick/unch/internal/termui"
)

type preparedEmbedder struct {
	Embedder      appembed.Embedder
	Provider      appembed.Provider
	ModelID       string
	ResolvedModel string
	ResolvedLib   string
	ContextSize   int
}

func prepareEmbedder(
	ctx context.Context,
	s *termui.Session,
	targetPaths semsearch.Paths,
	requestedProvider string,
	requestedModel string,
	requestedLib string,
	contextSize int,
	verbose bool,
	runtimes runtime.YzmaResolver,
	models runtime.ModelCache,
) (preparedEmbedder, error) {
	provider, err := appembed.ParseProvider(requestedProvider)
	if err != nil {
		return preparedEmbedder{}, err
	}

	switch provider {
	case appembed.ProviderOpenRouter:
		if strings.TrimSpace(requestedLib) != "" {
			return preparedEmbedder{}, fmt.Errorf("--lib is only supported with provider llama.cpp")
		}
		modelID := strings.TrimSpace(requestedModel)
		if modelID == "" {
			modelID = strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
		}
		if modelID == "" {
			return preparedEmbedder{}, fmt.Errorf("provider openrouter requires --model or OPENROUTER_MODEL")
		}
		apiKey, err := semsearch.ResolveProviderToken(targetPaths.LocalDir, provider.String())
		if err != nil {
			if s != nil {
				printSessionLine(s, "Warning: OpenRouter token is not configured. Run `unch auth openrouter --token <token>` or set OPENROUTER_API_KEY.")
			}
			return preparedEmbedder{}, fmt.Errorf("resolve openrouter token: %w", err)
		}

		embedder, err := loadEmbedderWithSpinner(ctx, s, func() (appembed.Embedder, error) {
			return openrouterembed.New(ctx, openrouterembed.Config{
				ModelID:     modelID,
				APIKey:      apiKey,
				BaseURL:     strings.TrimSpace(os.Getenv("OPENROUTER_BASE_URL")),
				HTTPReferer: strings.TrimSpace(os.Getenv("OPENROUTER_HTTP_REFERER")),
				AppTitle:    strings.TrimSpace(os.Getenv("OPENROUTER_APP_TITLE")),
				Formatter:   appembed.FormatterForModel(modelID),
			})
		})
		if err != nil {
			return preparedEmbedder{}, err
		}

		return preparedEmbedder{
			Embedder:      embedder,
			Provider:      provider,
			ModelID:       modelID,
			ResolvedModel: modelID,
		}, nil
	case appembed.ProviderLlamaCPP:
		defaultModelPath := runtime.DefaultModelPath(targetPaths.ModelsDir)
		requestedModelPath := strings.TrimSpace(requestedModel)
		if requestedModelPath == "" {
			requestedModelPath = defaultModelPath
		}

		resolvedLibPath, libNote, err := runtimes.ResolveOrInstallYzmaLibPath(ctx, strings.TrimSpace(requestedLib), targetPaths.LocalDir, s)
		if err != nil {
			return preparedEmbedder{}, err
		}
		if libNote != "" && s != nil {
			s.Logf("%s", libNote)
		}

		resolvedModelPath, modelNote, err := models.ResolveOrInstallModelPath(ctx, requestedModelPath, defaultModelPath, true, s)
		if err != nil {
			return preparedEmbedder{}, err
		}
		if modelNote != "" && s != nil {
			s.Logf("%s", modelNote)
		}

		modelID, err := runtime.CanonicalModelID(requestedModelPath, defaultModelPath)
		if err != nil {
			return preparedEmbedder{}, fmt.Errorf("resolve model id: %w", err)
		}

		resolvedContextSize := contextSize
		if resolvedContextSize <= 0 {
			resolvedContextSize = defaultContextSize(resolvedModelPath)
		}

		embedder, err := loadEmbedderWithSpinner(ctx, s, func() (appembed.Embedder, error) {
			return llamaembed.New(llamaembed.Config{
				ModelPath:   resolvedModelPath,
				LibPath:     resolvedLibPath,
				ContextSize: resolvedContextSize,
				Verbose:     verbose,
				Pooling:     defaultPooling(resolvedModelPath),
			})
		})
		if err != nil {
			return preparedEmbedder{}, err
		}

		return preparedEmbedder{
			Embedder:      embedder,
			Provider:      provider,
			ModelID:       modelID,
			ResolvedModel: resolvedModelPath,
			ResolvedLib:   resolvedLibPath,
			ContextSize:   resolvedContextSize,
		}, nil
	default:
		return preparedEmbedder{}, fmt.Errorf("unsupported embedding provider %q", provider)
	}
}
