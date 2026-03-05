package file

import (
	"os"
	"path/filepath"
	"strings"
)

func mkdirLocalDir(baseDir, dir, name string) (cleanDir string, cleanName string, code int, msg string) {
	cleanDir, err := sanitizeDir(dir)
	if err != nil {
		return "", "", 400, "invalid dir"
	}
	cleanName, err = sanitizeName(name)
	if err != nil {
		return cleanDir, "", 400, "invalid name"
	}
	finalPath, _, err := resolvePaths(baseDir, cleanDir, cleanName)
	if err != nil {
		return cleanDir, cleanName, 400, "invalid path"
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return cleanDir, cleanName, 500, "mkdir failed"
	}

	info, err := os.Stat(finalPath)
	if err == nil {
		if info != nil && info.IsDir() {
			return cleanDir, cleanName, 1, "ok"
		}
		return cleanDir, cleanName, 409, "exists"
	}
	if !os.IsNotExist(err) {
		return cleanDir, cleanName, 500, "stat failed"
	}

	if err := os.Mkdir(finalPath, 0o755); err != nil {
		if os.IsExist(err) {
			if st, serr := os.Stat(finalPath); serr == nil && st != nil && st.IsDir() {
				return cleanDir, cleanName, 1, "ok"
			}
			return cleanDir, cleanName, 409, "exists"
		}
		if strings.Contains(strings.ToLower(err.Error()), "not a directory") {
			return cleanDir, cleanName, 409, "exists"
		}
		return cleanDir, cleanName, 500, "mkdir failed"
	}
	return cleanDir, cleanName, 1, "ok"
}
