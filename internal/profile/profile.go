package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,39}$`)

type Profile struct {
	Name        string `json:"name"`
	ConfigPath  string `json:"configPath"`
	WorkingDir  string `json:"workingDir"`
	AutoConnect bool   `json:"autoConnect"`
	Reconnect   bool   `json:"reconnect"`
}

type Store struct {
	Root string
}

func DefaultRoot() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "TunnelDesk"), nil
}

func NewStore(root string) *Store { return &Store{Root: root} }

func NormalizeName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !validName.MatchString(name) {
		return "", errors.New("profile name must use 1-40 letters, numbers, dashes or underscores")
	}
	return name, nil
}

func (s *Store) Add(name, source string) (Profile, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return Profile{}, err
	}
	if !strings.EqualFold(filepath.Ext(source), ".ovpn") {
		return Profile{}, errors.New("configuration must be an .ovpn file")
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return Profile{}, fmt.Errorf("resolve configuration path: %w", err)
	}
	in, err := os.Open(source)
	if err != nil {
		return Profile{}, fmt.Errorf("open configuration: %w", err)
	}
	defer in.Close()

	dir := filepath.Join(s.Root, "profiles", name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Profile{}, err
	}
	target := filepath.Join(dir, "client.ovpn")
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return Profile{}, err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return Profile{}, copyErr
	}
	if closeErr != nil {
		return Profile{}, closeErr
	}
	p := Profile{
		Name:       name,
		ConfigPath: target,
		WorkingDir: filepath.Dir(source),
		Reconnect:  true,
	}
	return p, s.Save(p)
}

func (s *Store) Save(p Profile) error {
	name, err := NormalizeName(p.Name)
	if err != nil {
		return err
	}
	p.Name = name
	dir := filepath.Join(s.Root, "profiles", p.Name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "profile.json"), data, 0600)
}

func (s *Store) Get(name string) (Profile, error) {
	data, err := os.ReadFile(filepath.Join(s.Root, "profiles", strings.ToLower(name), "profile.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, fmt.Errorf("profile %q does not exist", name)
		}
		return Profile{}, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return Profile{}, err
	}
	if p.WorkingDir == "" {
		p.WorkingDir = filepath.Dir(p.ConfigPath)
	}
	return p, nil
}

func (s *Store) List() ([]Profile, error) {
	dir := filepath.Join(s.Root, "profiles")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var profiles []Profile
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p, err := s.Get(entry.Name())
		if err == nil {
			profiles = append(profiles, p)
		}
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

func (s *Store) Remove(name string) error {
	if _, err := s.Get(name); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.Root, "profiles", strings.ToLower(name)))
}

func (s *Store) LogPath(name string) string {
	return filepath.Join(s.Root, "profiles", strings.ToLower(name), "openvpn.log")
}

func (s *Store) StatePath(name string) string {
	return filepath.Join(s.Root, "profiles", strings.ToLower(name), "state.json")
}

func (s *Store) LockPath(name string) string {
	return filepath.Join(s.Root, "profiles", strings.ToLower(name), "connection.lock")
}
