package scan

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func canonicalDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return resolveFilesystemPath(resolved)
}

func relativePath(base, path string) (string, error) {
	relative, err := filepath.Rel(base, path)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path is outside the path base")
	}
	return filepath.ToSlash(relative), nil
}

// JSON strings cannot represent arbitrary Unix filename bytes losslessly.
func portableText(value string) (string, []byte) {
	if utf8.ValidString(value) {
		return value, nil
	}
	return strings.ToValidUTF8(value, "\uFFFD"), []byte(value)
}
