package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type BusyError struct {
	ID  string
	PID int
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("session %s is working in another mga process (pid %d); try again when that turn ends", e.ID, e.PID)
}

func lockPath(dir, id string) string { return filepath.Join(dir, id+".lock") }

func LockSession(dir, id string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := lockPath(dir, id)
	for range 2 {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			fmt.Fprint(f, os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if pid := LockedBy(dir, id); pid != 0 {
			return nil, &BusyError{ID: id, PID: pid}
		}
		os.Remove(path)
	}
	return nil, fmt.Errorf("could not lock session %s", id)
}

func LockedBy(dir, id string) int {
	data, err := os.ReadFile(lockPath(dir, id))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid == os.Getpid() || !processAlive(pid) {
		return 0
	}
	return pid
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (s *Session) Reload(dir string) (*Session, bool) {
	disk, err := LoadSession(dir, s.ID)
	if err != nil || !s.Updated.Before(disk.Updated) {
		return nil, false
	}
	return disk, true
}

func (s *Session) keepDiskMeta(dir string) {
	disk, err := LoadSession(dir, s.ID)
	if err != nil {
		return
	}
	s.Pinned, s.PinOrder, s.Order = disk.Pinned, disk.PinOrder, disk.Order
	if disk.Title != "" && (s.Title == "" || disk.TitleSource == TitleFromUser) {
		s.Title, s.TitleSource = disk.Title, disk.TitleSource
	}
}
