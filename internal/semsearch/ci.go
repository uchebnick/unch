package semsearch

import (
	"fmt"
	"os"
	"path/filepath"
)

const DefaultCIWorkflow = `name: searcher

on:
  push:
    branches:
      - main
  workflow_dispatch:

permissions:
  contents: read

jobs:
  index:
    name: build-search-index
    runs-on: macos-14

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install unch
        shell: bash
        run: |
          set -euo pipefail
          brew install uchebnick/tap/unch
          echo "::group::Tooling"
          command -v unch
          unch init --help
          echo "::endgroup::"

      - name: Build local search index
        shell: bash
        run: |
          set -euo pipefail
          mkdir -p .semsearch/logs
          echo "::group::unch init"
          unch init --root .
          echo "::endgroup::"
          echo "::group::unch index"
          unch index --root . 2>&1 | tee .semsearch/logs/searcher-index.log
          echo "::endgroup::"
          echo "::group::Generated search artifacts"
          find .semsearch -maxdepth 2 -type f | sort
          echo
          ls -lah .semsearch
          echo "::endgroup::"
          echo "::group::Manifest"
          cat .semsearch/manifest.json
          echo "::endgroup::"

      - name: Render GitHub summary
        if: ${{ always() }}
        shell: bash
        run: |
          set -euo pipefail
          {
            echo "## Search Index"
            echo
            echo "- Repository: <code>${GITHUB_REPOSITORY}</code>"
            echo "- Ref: <code>${GITHUB_REF_NAME}</code>"
            echo "- Commit: <code>${GITHUB_SHA::7}</code>"
            echo
            echo "### Artifact contents"
            echo
            echo '<pre>'
            if [ -d .semsearch ]; then
              find .semsearch -maxdepth 2 -type f | sort
            else
              echo "No .semsearch directory was generated."
            fi
            echo '</pre>'
            echo
            echo "### Manifest"
            echo
            echo '<pre>'
            if [ -f .semsearch/manifest.json ]; then
              cat .semsearch/manifest.json
            else
              echo "{}"
            fi
            echo '</pre>'
            if [ -f .semsearch/logs/searcher-index.log ]; then
              echo
              echo "### Index log tail"
              echo
              echo '<pre>'
              tail -n 80 .semsearch/logs/searcher-index.log
              echo '</pre>'
            fi
          } >> "$GITHUB_STEP_SUMMARY"

      - name: Upload search index
        if: ${{ always() }}
        uses: actions/upload-artifact@v4
        with:
          name: semsearch-index
          path: |
            .semsearch/index.db
            .semsearch/manifest.json
            .semsearch/logs/
          if-no-files-found: error
`

func CIWorkflowPath(root string) string {
	return filepath.Join(root, ".github", "workflows", "searcher.yml")
}

func EnsureCIWorkflow(root string) (string, bool, error) {
	path := CIWorkflowPath(root)
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !os.IsNotExist(err) {
		return "", false, fmt.Errorf("stat %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(DefaultCIWorkflow), 0o644); err != nil {
		return "", false, fmt.Errorf("write %s: %w", path, err)
	}
	return path, true, nil
}
