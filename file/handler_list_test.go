package file

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleListLocal_RootAutoCreatesBaseDir(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "file")
	if _, err := os.Stat(baseDir); err == nil {
		t.Fatalf("expected baseDir not exist, got exists: %q", baseDir)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat baseDir failed: %v", err)
	}

	h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.handleListLocal(context.Background(), nil, readReq{Dir: ""}, handlerConfig{BaseDir: baseDir})

	st, err := os.Stat(baseDir)
	if err != nil {
		t.Fatalf("baseDir should be created, stat failed: %v", err)
	}
	if !st.IsDir() {
		t.Fatalf("baseDir should be a directory: %q", baseDir)
	}
}

func TestHandleListLocal_SubDirDoesNotCreateBaseDir(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "file")

	h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.handleListLocal(context.Background(), nil, readReq{Dir: "sub"}, handlerConfig{BaseDir: baseDir})

	_, err := os.Stat(baseDir)
	if err == nil {
		t.Fatalf("baseDir should not be created when listing subdir: %q", baseDir)
	}
	if !os.IsNotExist(err) {
		t.Fatalf("stat baseDir failed: %v", err)
	}
}
