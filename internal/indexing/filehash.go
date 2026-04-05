package indexing

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cespare/xxhash/v2"
)

const fileHashFingerprintVersion = "index-input-v1"

type HashedFile struct {
	Path        string
	SourcePath  string
	ContentHash string
}

func CollectHashedFiles(root string, gitignorePath string, extraPatterns []string) ([]HashedFile, map[string]string, error) {
	files := make([]HashedFile, 0)
	hashes := make(map[string]string)

	err := walkIndexedPaths(root, gitignorePath, extraPatterns, func(path string, rel string) error {
		contentHash, binary, err := hashSourceFile(path)
		if err != nil {
			return err
		}
		if binary {
			return nil
		}

		files = append(files, HashedFile{
			Path:        rel,
			SourcePath:  path,
			ContentHash: contentHash,
		})
		hashes[rel] = contentHash
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return files, hashes, nil
}

func CollectFileHashes(root string, gitignorePath string, extraPatterns []string) (map[string]string, error) {
	_, hashes, err := CollectHashedFiles(root, gitignorePath, extraPatterns)
	if err != nil {
		return nil, err
	}
	return hashes, nil
}

func BuildScannerFingerprint(commentPrefix string, contextPrefix string, excludes []string) string {
	normalizedExcludes := append([]string(nil), excludes...)
	sort.Strings(normalizedExcludes)

	payload := strings.Join([]string{
		fileHashFingerprintVersion,
		strings.TrimSpace(commentPrefix),
		strings.TrimSpace(contextPrefix),
		strings.Join(normalizedExcludes, "\x00"),
	}, "\x1f")

	return fmt.Sprintf("%s:%016x", fileHashFingerprintVersion, xxhash.Sum64String(payload))
}
