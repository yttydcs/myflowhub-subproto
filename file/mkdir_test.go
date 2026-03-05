package file

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMkdirLocalDir_CreateSuccess(t *testing.T) {
	baseDir := t.TempDir()
	dir, name, code, msg := mkdirLocalDir(baseDir, "a/b", "newdir")
	if code != 1 {
		t.Fatalf("code = %d, want 1, msg=%s", code, msg)
	}
	if dir != "a/b" || name != "newdir" {
		t.Fatalf("dir/name = %q/%q, want %q/%q", dir, name, "a/b", "newdir")
	}
	target := filepath.Join(baseDir, "a", "b", "newdir")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target failed: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("target is not dir: %q", target)
	}
}

func TestMkdirLocalDir_IdempotentWhenDirExists(t *testing.T) {
	baseDir := t.TempDir()
	target := filepath.Join(baseDir, "already")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("prepare target dir failed: %v", err)
	}
	_, _, code, msg := mkdirLocalDir(baseDir, "", "already")
	if code != 1 {
		t.Fatalf("code = %d, want 1, msg=%s", code, msg)
	}
}

func TestMkdirLocalDir_ConflictWithFile(t *testing.T) {
	baseDir := t.TempDir()
	target := filepath.Join(baseDir, "conflict")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("prepare conflict file failed: %v", err)
	}
	_, _, code, msg := mkdirLocalDir(baseDir, "", "conflict")
	if code != 409 || msg != "exists" {
		t.Fatalf("code/msg = %d/%q, want 409/%q", code, msg, "exists")
	}
}

func TestMkdirLocalDir_InvalidInput(t *testing.T) {
	baseDir := t.TempDir()
	_, _, code, _ := mkdirLocalDir(baseDir, "../escape", "x")
	if code != 400 {
		t.Fatalf("dir invalid code = %d, want 400", code)
	}
	_, _, code, _ = mkdirLocalDir(baseDir, "", "../bad")
	if code != 400 {
		t.Fatalf("name invalid code = %d, want 400", code)
	}
}
