package openvpn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Felipyan19/tunneldesk/internal/profile"
)

func testManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source.ovpn")
	if err := os.WriteFile(source, []byte("client\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := profile.NewStore(filepath.Join(root, "data"))
	p, err := store.Add("Test", source)
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{Profiles: store}, p.Name
}

func TestUpdateSessionRejectsStaleRoutine(t *testing.T) {
	manager, name := testManager(t)
	initial := State{Profile: name, SessionID: "new", Status: StatusConnected}
	if err := manager.writeState(name, initial); err != nil {
		t.Fatal(err)
	}
	if err := manager.updateSession(name, "old", func(state *State) {
		state.Status = StatusDisconnected
	}); err != nil {
		t.Fatal(err)
	}
	state, err := manager.readState(name)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusConnected {
		t.Fatalf("stale session changed current state to %q", state.Status)
	}
}

func TestProfileLockIsExclusiveAndRecoverable(t *testing.T) {
	manager, name := testManager(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.writeState(name, State{
		Profile:    name,
		SessionID:  "one",
		PID:        os.Getpid(),
		BinaryPath: executable,
		Status:     StatusConnecting,
	}); err != nil {
		t.Fatal(err)
	}
	unlock, err := manager.acquireProfileLock(name, "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.acquireProfileLock(name, "two"); err == nil {
		t.Fatal("expected a second runner to be rejected")
	}
	unlock()
	if _, err := os.Stat(manager.Profiles.LockPath(name)); !os.IsNotExist(err) {
		t.Fatalf("lock file still exists: %v", err)
	}
}
