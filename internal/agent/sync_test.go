package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/shinucla/mini-golang-agent/internal/llm"
)

func TestLockSessionBlocksAnotherLiveProcess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s1.lock"), []byte(strconv.Itoa(os.Getppid())), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LockSession(dir, "s1")
	var busy *BusyError
	if !errors.As(err, &busy) || busy.PID != os.Getppid() || LockedBy(dir, "s1") != os.Getppid() {
		t.Fatalf("a lock held by a live process blocks: %v", err)
	}
}

func TestLockSessionReplacesALockFromADeadProcess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s1.lock"), []byte("999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	if LockedBy(dir, "s1") != 0 {
		t.Fatal("a dead process holds no lock")
	}
	release, err := LockSession(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := os.Stat(filepath.Join(dir, "s1.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("release removes the lock file")
	}
}

func TestSaveKeepsMetadataThatAnotherProcessChanged(t *testing.T) {
	dir := t.TempDir()
	mine := NewSession("/w")
	mine.Messages = []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	if err := mine.Save(dir); err != nil {
		t.Fatal(err)
	}
	other, err := LoadSession(dir, mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.SetPin(dir, true, 3); err != nil {
		t.Fatal(err)
	}
	if err := other.Rename(dir, "named elsewhere"); err != nil {
		t.Fatal(err)
	}
	mine.Messages = append(mine.Messages, llm.Message{Role: llm.RoleAssistant, Content: "hello"})
	if err := mine.Save(dir); err != nil {
		t.Fatal(err)
	}
	saved, _ := LoadSession(dir, mine.ID)
	if !saved.Pinned || saved.PinOrder != 3 || saved.Title != "named elsewhere" || len(saved.Messages) != 2 {
		t.Fatalf("saved = %+v", saved)
	}
	if _, ok := mine.Reload(dir); ok {
		t.Fatal("a session is not newer than its own save")
	}
	other.Messages = append(saved.Messages, llm.Message{Role: llm.RoleUser, Content: "more"})
	if err := other.Save(dir); err != nil {
		t.Fatal(err)
	}
	if disk, ok := mine.Reload(dir); !ok || len(disk.Messages) != 3 {
		t.Fatal("Reload returns a newer turn from another process")
	}
}
