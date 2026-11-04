package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type State string

const (
	NeedsInput State = "needs_input"
	Running    State = "running"
	DoneUnseen State = "done_unseen"
	Idle       State = "idle"
)

// Rank orders states by how much they want the user's attention.
func (s State) Rank() int {
	switch s {
	case NeedsInput:
		return 0
	case DoneUnseen:
		return 1
	case Running:
		return 2
	default:
		return 3
	}
}

func (s State) Valid() bool {
	switch s {
	case NeedsInput, Running, DoneUnseen, Idle:
		return true
	}
	return false
}

type Record struct {
	PaneID    string    `json:"pane_id"`
	ServerPID int       `json:"server_pid"`
	Agent     string    `json:"agent"`
	State     State     `json:"state"`
	SessionID string    `json:"session_id,omitempty"`
	Prompt    string    `json:"prompt,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	LastEvent time.Time `json:"last_event_at"`
	LastFocus time.Time `json:"last_focus_at"`
}

func (r Record) LastUsed() time.Time {
	if r.LastFocus.After(r.LastEvent) {
		return r.LastFocus
	}
	return r.LastEvent
}

type Store struct {
	Root string
}

func DefaultRoot() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "amx")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "amx")
	}
	return filepath.Join(home, ".local", "state", "amx")
}

func NewStore(root string) *Store {
	if root == "" {
		root = DefaultRoot()
	}
	return &Store{Root: root}
}

func (s *Store) serverDir(pid int) string {
	return filepath.Join(s.Root, "servers", strconv.Itoa(pid))
}

func (s *Store) path(pid int, paneID string) string {
	return filepath.Join(s.serverDir(pid), paneID+".json")
}

func (s *Store) Load(pid int) (map[string]Record, error) {
	out := map[string]Record{}
	entries, err := os.ReadDir(s.serverDir(pid))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return out, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.serverDir(pid), e.Name()))
		if err != nil {
			continue
		}
		var rec Record
		if json.Unmarshal(data, &rec) != nil || rec.PaneID == "" || rec.ServerPID != pid {
			continue
		}
		if !rec.State.Valid() {
			rec.State = Idle
		}
		out[rec.PaneID] = rec
	}
	return out, nil
}

func (s *Store) Save(rec Record) error {
	dir := s.serverDir(rec.ServerPID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, s.path(rec.ServerPID, rec.PaneID))
}

func (s *Store) Delete(pid int, paneID string) error {
	err := os.Remove(s.path(pid, paneID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) Get(pid int, paneID string) (*Record, error) {
	data, err := os.ReadFile(s.path(pid, paneID))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, nil
	}
	if rec.PaneID == "" || rec.ServerPID != pid {
		return nil, nil
	}
	if !rec.State.Valid() {
		rec.State = Idle
	}
	return &rec, nil
}

// PruneServer removes records for panes that no longer exist.
func (s *Store) PruneServer(pid int, live map[string]bool) {
	records, _ := s.Load(pid)
	for paneID := range records {
		if !live[paneID] {
			_ = s.Delete(pid, paneID)
		}
	}
}

// PruneServers removes state directories belonging to tmux servers that are
// no longer running. Pane ids are only unique per server, so this is what
// stops a restarted server from inheriting old statuses.
func (s *Store) PruneServers(current int) {
	root := filepath.Join(s.Root, "servers")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == current {
			continue
		}
		if processAlive(pid) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, e.Name()))
	}
}

// PruneOld drops records untouched for the given age.
func (s *Store) PruneOld(maxAge time.Duration, now time.Time) {
	root := filepath.Join(s.Root, "servers")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if now.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(path)
		}
		return nil
	})
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
