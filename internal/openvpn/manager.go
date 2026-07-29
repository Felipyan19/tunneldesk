package openvpn

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Felipyan19/tunneldesk/internal/profile"
	"github.com/Felipyan19/tunneldesk/internal/vault"
)

const (
	StatusConnecting   = "connecting"
	StatusConnected    = "connected"
	StatusDisconnected = "disconnected"
	StatusFailed       = "failed"
)

type State struct {
	Profile       string    `json:"profile"`
	SessionID     string    `json:"sessionId"`
	PID           int       `json:"pid"`
	BinaryPath    string    `json:"binaryPath,omitempty"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	StopRequested bool      `json:"stopRequested,omitempty"`
	StartedAt     time.Time `json:"startedAt,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Manager struct {
	Profiles *profile.Store
	Vault    vault.Vault
	Binary   string
	mu       sync.Mutex
}

func FindBinary() string {
	if configured := os.Getenv("TUNNELDESK_OPENVPN"); configured != "" {
		return configured
	}
	if path, err := exec.LookPath("openvpn"); err == nil {
		return path
	}
	if runtime.GOOS == "windows" {
		candidates := []string{
			filepath.Join(os.Getenv("ProgramFiles"), "OpenVPN", "bin", "openvpn.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "OpenVPN Connect", "openvpn.exe"),
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return "openvpn"
}

func NewSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// Connect starts one OpenVPN process. The caller owns the runner lifecycle and
// must keep running until the resulting state becomes disconnected or failed.
func (m *Manager) Connect(name, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if runtime.GOOS != "windows" {
		return errors.New("connections are currently supported only on Windows")
	}
	p, err := m.Profiles.Get(name)
	if err != nil {
		return err
	}
	name = p.Name
	if sessionID == "" {
		return errors.New("session ID is required")
	}

	unlock, err := m.acquireProfileLock(name, sessionID)
	if err != nil {
		return err
	}
	releaseLock := true
	defer func() {
		if releaseLock {
			unlock()
		}
	}()

	credentials, err := m.Vault.Get(name)
	if err != nil {
		return err
	}
	if strings.ContainsAny(credentials.Username, "\r\n") || strings.ContainsAny(credentials.Password, "\r\n") {
		return errors.New("credentials cannot contain line breaks")
	}

	logFile, err := os.OpenFile(m.Profiles.LogPath(name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	cmd := exec.Command(m.Binary, "--config", p.ConfigPath, "--auth-user-pass", "--auth-nocache")
	cmd.Dir = p.WorkingDir
	cmd.Stdin = strings.NewReader(credentials.Username + "\n" + credentials.Password + "\n")
	prepareProcess(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logFile.Close()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logFile.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start OpenVPN (%s): %w", m.Binary, err)
	}

	binaryPath := m.Binary
	if resolved, resolveErr := exec.LookPath(m.Binary); resolveErr == nil {
		binaryPath = resolved
	}
	if resolved, resolveErr := filepath.Abs(binaryPath); resolveErr == nil {
		binaryPath = resolved
	}
	state := State{
		Profile:    name,
		SessionID:  sessionID,
		PID:        cmd.Process.Pid,
		BinaryPath: binaryPath,
		Status:     StatusConnecting,
		StartedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := m.writeState(name, state); err != nil {
		_ = forceStop(cmd.Process.Pid)
		logFile.Close()
		return err
	}

	releaseLock = false
	go m.consume(name, sessionID, cmd, logFile, stdout, stderr, unlock)
	return nil
}

func (m *Manager) consume(
	name, sessionID string,
	cmd *exec.Cmd,
	logFile *os.File,
	stdout, stderr io.Reader,
	unlock func(),
) {
	defer logFile.Close()
	defer unlock()

	var scanWG sync.WaitGroup
	var logMu sync.Mutex
	scan := func(output io.Reader) {
		defer scanWG.Done()
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			line := redact(scanner.Text())
			logMu.Lock()
			_, _ = fmt.Fprintln(logFile, line)
			logMu.Unlock()
			if strings.Contains(line, "Initialization Sequence Completed") {
				_ = m.updateSession(name, sessionID, func(state *State) {
					state.Status = StatusConnected
					state.Error = ""
				})
			}
		}
	}

	scanWG.Add(2)
	go scan(stdout)
	go scan(stderr)
	scanWG.Wait()
	waitErr := cmd.Wait()
	_ = m.updateSession(name, sessionID, func(state *State) {
		state.PID = 0
		if state.StopRequested || waitErr == nil {
			state.Status = StatusDisconnected
			state.Error = ""
			return
		}
		state.Status = StatusFailed
		state.Error = waitErr.Error()
	})
	if waitErr != nil {
		logMu.Lock()
		_, _ = fmt.Fprintln(logFile, "OpenVPN exited:", waitErr)
		logMu.Unlock()
	}
}

func redact(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "password") || strings.Contains(lower, "auth-user-pass") {
		return "[sensitive OpenVPN log entry redacted]"
	}
	return line
}

func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, err := m.Profiles.Get(name)
	if err != nil {
		return err
	}
	name = p.Name
	state, err := m.readState(name)
	if err != nil || state.PID <= 0 || state.Status == StatusDisconnected || state.Status == StatusFailed {
		return fmt.Errorf("profile %q is not running", name)
	}
	if !processMatches(state.PID, state.BinaryPath) {
		return fmt.Errorf("refusing to stop PID %d because it is not the OpenVPN process started by TunnelDesk", state.PID)
	}
	state.StopRequested = true
	if err := m.writeState(name, state); err != nil {
		return err
	}
	if err := gracefulStop(state.PID, 5*time.Second); err != nil {
		return fmt.Errorf("stop OpenVPN: %w", err)
	}
	return nil
}

func (m *Manager) Status(name string) (State, error) {
	p, err := m.Profiles.Get(name)
	if err != nil {
		return State{}, err
	}
	name = p.Name
	state, err := m.readState(name)
	if os.IsNotExist(err) {
		return State{Profile: name, Status: StatusDisconnected}, nil
	}
	if err != nil {
		return State{}, err
	}
	if state.PID > 0 && state.Status != StatusDisconnected && state.Status != StatusFailed &&
		!processMatches(state.PID, state.BinaryPath) {
		_ = m.updateSession(name, state.SessionID, func(current *State) {
			current.PID = 0
			current.Status = StatusFailed
			current.Error = "the supervised OpenVPN process is no longer running"
		})
		return m.readState(name)
	}
	return state, nil
}

func (m *Manager) WaitForResult(name, sessionID string, timeout time.Duration) (State, error) {
	deadline := time.Now().Add(timeout)
	for {
		state, err := m.Status(name)
		if err != nil {
			return State{}, err
		}
		if state.SessionID == sessionID {
			switch state.Status {
			case StatusConnected:
				return state, nil
			case StatusFailed, StatusDisconnected:
				if state.Error != "" {
					return state, errors.New(state.Error)
				}
				return state, fmt.Errorf("OpenVPN stopped before connecting")
			}
		}
		if time.Now().After(deadline) {
			return state, fmt.Errorf("OpenVPN did not confirm the connection within %s; check `tunneldesk logs --%s`", timeout, name)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (m *Manager) RecordFailure(name, sessionID string, failure error) {
	p, err := m.Profiles.Get(name)
	if err != nil {
		return
	}
	current, readErr := m.readState(p.Name)
	if readErr == nil && current.SessionID != sessionID && current.PID > 0 &&
		processMatches(current.PID, current.BinaryPath) {
		return
	}
	_ = m.writeState(p.Name, State{
		Profile:   p.Name,
		SessionID: sessionID,
		Status:    StatusFailed,
		Error:     failure.Error(),
	})
}

func (m *Manager) readState(name string) (State, error) {
	data, err := os.ReadFile(m.Profiles.StatePath(name))
	if err != nil {
		return State{}, err
	}
	var state State
	err = json.Unmarshal(data, &state)
	return state, err
}

func (m *Manager) updateSession(name, sessionID string, change func(*State)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.readState(name)
	if err != nil {
		return err
	}
	if state.SessionID != sessionID {
		return nil
	}
	change(&state)
	return m.writeState(name, state)
}

func (m *Manager) writeState(name string, state State) error {
	state.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := m.Profiles.StatePath(name)
	temp, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceFile(tempPath, path)
}

func (m *Manager) acquireProfileLock(name, sessionID string) (func(), error) {
	path := m.Profiles.LockPath(name)
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_, _ = io.WriteString(file, sessionID)
			_ = file.Close()
			var once sync.Once
			return func() { once.Do(func() { _ = os.Remove(path) }) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		state, stateErr := m.readState(name)
		if stateErr == nil && state.PID > 0 && processMatches(state.PID, state.BinaryPath) {
			return nil, fmt.Errorf("profile %q is already running", name)
		}
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("profile %q is locked: %w", name, removeErr)
		}
	}
	return nil, fmt.Errorf("profile %q is already starting", name)
}

func (m *Manager) Logs(name string, lines int) (string, error) {
	p, err := m.Profiles.Get(name)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(m.Profiles.LogPath(p.Name))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	all := strings.Split(strings.TrimSpace(string(data)), "\n")
	if lines > 0 && len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n"), nil
}
