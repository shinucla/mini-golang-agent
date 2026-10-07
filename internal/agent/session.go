package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kzhuang/mini-golang-agent/internal/llm"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

type Session struct {
	ID       string        `json:"id"`
	Cwd      string        `json:"cwd"`
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Title    string        `json:"title"`
	Created  time.Time     `json:"created"`
	Updated  time.Time     `json:"updated"`
	Messages []llm.Message `json:"messages"`
}

func NewSession(cwd string) *Session {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	now := time.Now()
	return &Session{ID: now.Format("20060102-150405") + "-" + hex.EncodeToString(b), Cwd: cwd, Created: now}
}

func (s *Session) Save(dir string) error {
	if len(s.Messages) == 0 {
		return nil
	}
	if s.Title == "" {
		for _, m := range s.Messages {
			if m.Role == llm.RoleUser {
				s.Title = tools.OneLine(m.Content, 80)
				break
			}
		}
	}
	s.Updated = time.Now()
	return s.write(dir)
}

func (s *Session) Rename(dir, title string) error {
	s.Title = strings.TrimSpace(title)
	if len(s.Messages) == 0 {
		return nil
	}
	return s.write(dir)
}

func DeleteSession(dir, id string) error {
	err := os.Remove(filepath.Join(dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Session) write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, s.ID+".json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, s.ID+".json"))
}

func LoadSession(dir, id string) (*Session, error) {
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func ListSessions(dir, cwd string) ([]*Session, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Session
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		s, err := LoadSession(dir, id)
		if err != nil || (cwd != "" && s.Cwd != cwd) {
			continue
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b *Session) int { return b.Updated.Compare(a.Updated) })
	return out, nil
}
