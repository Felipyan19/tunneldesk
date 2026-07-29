package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddGetListRemove(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.ovpn")
	if err := os.WriteFile(source, []byte("client\nremote vpn.example.com 1194\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(root, "data"))
	added, err := store.Add("work-vpn", source)
	if err != nil {
		t.Fatal(err)
	}
	if added.Name != "work-vpn" {
		t.Fatalf("expected normalized name work-vpn, got %s", added.Name)
	}
	if added.WorkingDir != root {
		t.Fatalf("expected working directory %q, got %q", root, added.WorkingDir)
	}
	got, err := store.Get("work-vpn")
	if err != nil || got.ConfigPath == "" {
		t.Fatalf("get profile: %#v, %v", got, err)
	}
	profiles, err := store.List()
	if err != nil || len(profiles) != 1 {
		t.Fatalf("list profiles: %#v, %v", profiles, err)
	}
	if err := store.Remove("work-vpn"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("work-vpn"); err == nil {
		t.Fatal("expected removed profile to be missing")
	}
}

func TestRejectsInvalidProfile(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Add("../escape", "client.ovpn"); err == nil {
		t.Fatal("expected invalid profile name to be rejected")
	}
	if _, err := store.Add("valid", "client.txt"); err == nil {
		t.Fatal("expected non-ovpn file to be rejected")
	}
}
