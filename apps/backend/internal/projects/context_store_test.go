package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	storageworkspaces "github.com/kandev/kandev/internal/system/storage/workspaces"
)

func TestContextStoreProvisionAndHashCheckedWrite(t *testing.T) {
	store := NewContextStore(filepath.Join(t.TempDir(), "agent-projects"))
	projectID := uuid.NewString()
	root, err := store.Provision(context.Background(), projectID)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "notes.md")); err != nil || string(got) != "# Project notes\n" {
		t.Fatalf("initial notes = %q, err = %v", got, err)
	}

	content, hash, err := store.ReadFile(projectID, "notes.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	newHash, err := store.WriteFile(projectID, "notes.md", hash, []byte("shared note\n"))
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if newHash == hash || content != "# Project notes\n" {
		t.Fatalf("content/hash = %q/%q, previous hash %q", content, newHash, hash)
	}
	if _, err := store.WriteFile(projectID, "notes.md", hash, []byte("stale write\n")); !errors.Is(err, ErrContextConflict) {
		t.Fatalf("stale WriteFile error = %v, want ErrContextConflict", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "notes.md")); err != nil || string(got) != "shared note\n" {
		t.Fatalf("notes after stale write = %q, err = %v", got, err)
	}
}

func TestContextStoreRejectsTraversalAndSymlinkComponents(t *testing.T) {
	store := NewContextStore(filepath.Join(t.TempDir(), "agent-projects"))
	projectID := uuid.NewString()
	root, err := store.Provision(context.Background(), projectID)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if _, _, err := store.ReadFile(projectID, "../notes.md"); !errors.Is(err, ErrInvalidContextPath) {
		t.Fatalf("traversal ReadFile error = %v, want ErrInvalidContextPath", err)
	}
	escape := t.TempDir()
	if err := os.WriteFile(filepath.Join(escape, "secret.md"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(escape, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReadFile(projectID, "linked/secret.md"); !errors.Is(err, ErrInvalidContextPath) {
		t.Fatalf("symlink ReadFile error = %v, want ErrInvalidContextPath", err)
	}
}

func TestContextStoreListsWithoutFollowingSymlinks(t *testing.T) {
	store := NewContextStore(filepath.Join(t.TempDir(), "agent-projects"))
	projectID := uuid.NewString()
	root, err := store.Provision(context.Background(), projectID)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("guide"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(projectID, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 || entries[0].Name != "docs" || entries[1].Name != "notes.md" {
		t.Fatalf("List entries = %#v, want docs and notes.md", entries)
	}
}

func TestContextStoreSharesCanonicalFilesAcrossTaskLinks(t *testing.T) {
	base := t.TempDir()
	store := NewContextStore(filepath.Join(base, "agent-projects"))
	projectID := uuid.NewString()
	contextRoot, err := store.Provision(context.Background(), projectID)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	for _, task := range []struct{ id, dir string }{{"coordinator", "coordinator_abc"}, {"worker", "worker_def"}} {
		taskRoot := filepath.Join(base, "tasks", task.dir)
		if err := os.MkdirAll(taskRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := storageworkspaces.WriteOwnershipMarker(taskRoot, storageworkspaces.OwnershipMarker{
			TaskID: task.id, WorkspaceID: "workspace-1", TaskDirName: task.dir,
			LayoutVersion: storageworkspaces.LayoutVersionSemantic,
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.EnsureTaskLink(taskRoot, projectID, task.id, task.dir); err != nil {
			t.Fatalf("EnsureTaskLink(%s): %v", task.id, err)
		}
	}

	workerNotes := filepath.Join(base, "tasks", "worker_def", "context", "notes.md")
	if err := os.WriteFile(workerNotes, []byte("worker update\n"), 0o600); err != nil {
		t.Fatalf("worker direct write: %v", err)
	}
	coordinatorNotes := filepath.Join(base, "tasks", "coordinator_abc", "context", "notes.md")
	got, err := os.ReadFile(coordinatorNotes)
	if err != nil || string(got) != "worker update\n" {
		t.Fatalf("coordinator sees shared context %q, err %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(contextRoot, "notes.md")); err != nil {
		t.Fatalf("canonical notes file is missing: %v", err)
	}
}
