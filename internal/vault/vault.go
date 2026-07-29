package vault

import "errors"

var ErrUnsupported = errors.New("secure credentials are supported only on Windows")

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Vault interface {
	Put(profile string, credentials Credentials) error
	Get(profile string) (Credentials, error)
	Delete(profile string) error
}

func New(root string) Vault { return newPlatformVault(root) }
