package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/4gust/ring0/internal/model"
)

// Store persists apps + routes to a JSON file under ~/.ring0/state.json.
type Store struct {
	mu     sync.RWMutex
	path   string
	Apps   []*model.App   `json:"apps"`
	Routes []*model.Route `json:"routes"`
}

func defaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ring0", "state.json")
}

func New() (*Store, error) {
	p := defaultPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: p}
	if data, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(data, s)
	}
	return s, nil
}

func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

// ---- Apps ----

func (s *Store) ListApps() []*model.App {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.App, len(s.Apps))
	copy(out, s.Apps)
	return out
}

func (s *Store) AddApp(a *model.App) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.Name == "" || a.Cmd == "" {
		return fmt.Errorf("name and cmd are required")
	}
	for _, ex := range s.Apps {
		if ex.Name == a.Name {
			return fmt.Errorf("app %q already exists", a.Name)
		}
		if a.Port != 0 && ex.Port == a.Port {
			return fmt.Errorf("port %d already in use by %q", a.Port, ex.Name)
		}
	}
	a.ID = fmt.Sprintf("app-%d", time.Now().UnixNano())
	a.Status = model.StatusStopped
	s.Apps = append(s.Apps, a)
	return nil
}

func (s *Store) RemoveApp(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Apps[:0]
	for _, a := range s.Apps {
		if a.ID != id {
			out = append(out, a)
		}
	}
	s.Apps = out
}

func (s *Store) FindApp(id string) *model.App {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.Apps {
		if a.ID == id {
			return a
		}
	}
	return nil
}

// ---- Routes ----

func (s *Store) ListRoutes() []*model.Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*model.Route, len(s.Routes))
	copy(out, s.Routes)
	return out
}

func (s *Store) AddRoute(r *model.Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Path == "" {
		return fmt.Errorf("path is required")
	}
	if r.TargetPort == 0 {
		return fmt.Errorf("target port is required")
	}
	for _, ex := range s.Routes {
		if ex.Path == r.Path && ex.Host == r.Host {
			return fmt.Errorf("route conflict with %s%s", r.Host, r.Path)
		}
	}
	if r.Visibility == "" {
		r.Visibility = model.Private
	}
	r.ID = fmt.Sprintf("rt-%d", time.Now().UnixNano())
	s.Routes = append(s.Routes, r)
	return nil
}

func (s *Store) RemoveRoute(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Routes[:0]
	for _, r := range s.Routes {
		if r.ID != id {
			out = append(out, r)
		}
	}
	s.Routes = out
}

func (s *Store) UpdateRoute(r *model.Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, ex := range s.Routes {
		if ex.ID == r.ID {
			s.Routes[i] = r
			return nil
		}
	}
	return fmt.Errorf("route not found")
}
