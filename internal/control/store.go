package control

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	mu    sync.RWMutex
	state State
	dir   string
}

func OpenStore(dir string) (*Store, error) {
	return openStore(dir, nil)
}

func openStore(dir string, seed func(*State) error) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, state: initialState()}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil {
		if err = json.Unmarshal(data, &s.state); err != nil {
			return nil, fmt.Errorf("读取数据失败，原文件已保留: %w", err)
		}
		if s.state.Version != 1 {
			return nil, fmt.Errorf("不支持的数据版本 %d", s.state.Version)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	} else {
		if seed != nil {
			if err := seed(&s.state); err != nil {
				return nil, err
			}
		}
		if err := s.Update(func(st *State) error { st.event("info", "PortPilot 已就绪，添加订阅即可开始"); return nil }); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func clone(st State) State {
	data, _ := json.Marshal(st)
	var out State
	_ = json.Unmarshal(data, &out)
	return out
}
func (s *Store) Snapshot() State { s.mu.RLock(); defer s.mu.RUnlock(); return clone(s.state) }

func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.state)
	if err := fn(&next); err != nil {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(s.dir, "state.json"), data); err != nil {
		return err
	}
	s.state = next
	return nil
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".portpilot-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
