//go:build windows

package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type dataBlob struct {
	size uint32
	data *byte
}

type platformVault struct {
	root string
}

var (
	crypt32          = syscall.NewLazyDLL("crypt32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	cryptProtectData = crypt32.NewProc("CryptProtectData")
	cryptUnprotect   = crypt32.NewProc("CryptUnprotectData")
	localFree        = kernel32.NewProc("LocalFree")
)

func newPlatformVault(root string) Vault { return &platformVault{root: root} }

func blob(data []byte) dataBlob {
	if len(data) == 0 {
		return dataBlob{}
	}
	return dataBlob{size: uint32(len(data)), data: &data[0]}
}

func bytesFromBlob(b dataBlob) []byte {
	if b.size == 0 || b.data == nil {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(b.data, b.size)...)
}

func protect(data []byte) ([]byte, error) {
	in := blob(data)
	var out dataBlob
	ok, _, callErr := cryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return nil, fmt.Errorf("DPAPI encryption failed: %w", callErr)
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.data)))
	return bytesFromBlob(out), nil
}

func unprotect(data []byte) ([]byte, error) {
	in := blob(data)
	var out dataBlob
	ok, _, callErr := cryptUnprotect.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return nil, fmt.Errorf("DPAPI decryption failed: %w", callErr)
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.data)))
	return bytesFromBlob(out), nil
}

func (v *platformVault) path(profile string) string {
	return filepath.Join(v.root, "profiles", canonicalProfile(profile), "credentials.dpapi")
}

func canonicalProfile(profile string) string {
	return strings.ToLower(strings.TrimSpace(profile))
}

func (v *platformVault) Put(profile string, credentials Credentials) error {
	raw, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	encrypted, err := protect(raw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(v.path(profile)), 0700); err != nil {
		return err
	}
	return os.WriteFile(v.path(profile), encrypted, 0600)
}

func (v *platformVault) Get(profile string) (Credentials, error) {
	encrypted, err := os.ReadFile(v.path(profile))
	if err != nil {
		return Credentials{}, fmt.Errorf("credentials not configured for %q", profile)
	}
	raw, err := unprotect(encrypted)
	if err != nil {
		return Credentials{}, err
	}
	var credentials Credentials
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return Credentials{}, err
	}
	return credentials, nil
}

func (v *platformVault) Delete(profile string) error {
	err := os.Remove(v.path(profile))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
