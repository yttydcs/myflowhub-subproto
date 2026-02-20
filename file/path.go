package file

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	errInvalidName = errors.New("invalid name")
	errInvalidDir  = errors.New("invalid dir")
)

func sanitizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", errInvalidName
	}
	if strings.ContainsAny(name, "/\\") {
		return "", errInvalidName
	}
	if strings.ContainsRune(name, 0) {
		return "", errInvalidName
	}
	return name, nil
}

func sanitizeDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" || dir == "." {
		return "", nil
	}
	if strings.ContainsRune(dir, 0) {
		return "", errInvalidDir
	}
	// 拒绝绝对路径与盘符
	if strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "\\") {
		return "", errInvalidDir
	}
	if len(dir) >= 2 {
		// C: / C:\ / C:foo
		if ((dir[0] >= 'a' && dir[0] <= 'z') || (dir[0] >= 'A' && dir[0] <= 'Z')) && dir[1] == ':' {
			return "", errInvalidDir
		}
	}
	// 统一使用 '/' 进行清洗与分段判定（跨平台一致）
	if strings.Contains(dir, "\\") {
		return "", errInvalidDir
	}
	clean := path.Clean(dir)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errInvalidDir
	}
	return clean, nil
}

func resolvePaths(baseDir, dir, name string) (finalPath, partPath string, err error) {
	name, err = sanitizeName(name)
	if err != nil {
		return "", "", err
	}
	dir, err = sanitizeDir(dir)
	if err != nil {
		return "", "", err
	}

	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = "."
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", "", err
	}
	absFinal, err := filepath.Abs(filepath.Join(absBase, filepath.FromSlash(dir), name))
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(absBase, absFinal)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", errInvalidDir
	}
	return absFinal, absFinal + ".part", nil
}
