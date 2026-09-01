package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathGuardRejectsTraversalAndEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Resolve(root, "../outside", false); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Resolve(root, "escape", true); err == nil {
		t.Fatal("expected escaping symlink to be rejected")
	}
}

func TestFileServiceUsesRecoverableTrash(t *testing.T) {
	root := t.TempDir()
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewFileService(guard, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.WriteContent(root, "app.js", []byte("console.log('ok')\n")); err != nil {
		t.Fatal(err)
	}
	destination, err := service.Trash(root, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, destination)); err != nil {
		t.Fatalf("trashed file is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "app.js")); !os.IsNotExist(err) {
		t.Fatal("source file should have moved")
	}
}

func TestFileServiceRejectsSymlinkedTrashAndOutsideReads(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewFileService(guard, "")
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ReadContent(root, "escape.txt"); err == nil {
		t.Fatal("outside file was readable through a symlink")
	}
	if err := service.WriteContent(root, "app.js", []byte("ok\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".serverpal-trash")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Trash(root, "app.js"); err == nil {
		t.Fatal("symlinked trash directory was accepted")
	}
}

func TestUploadDoesNotReplaceExistingFile(t *testing.T) {
	root := t.TempDir()
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewFileService(guard, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.WriteContent(root, "app.js", []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := service.Upload(root, "app.js", strings.NewReader("replacement")); err == nil {
		t.Fatal("upload replaced an existing file")
	}
	content, _, err := service.ReadContent(root, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Fatalf("existing content changed to %q", content)
	}
}

func TestValidateHostname(t *testing.T) {
	valid := []string{"panel.example.com", "api-1.example.co.uk"}
	for _, value := range valid {
		if _, err := ValidateHostname(value); err != nil {
			t.Errorf("expected %q to be valid: %v", value, err)
		}
	}
	invalid := []string{"localhost", "127.0.0.1", "bad domain.com", "example.com/path", "-api.example.com"}
	for _, value := range invalid {
		if _, err := ValidateHostname(value); err == nil {
			t.Errorf("expected %q to be invalid", value)
		}
	}
}
