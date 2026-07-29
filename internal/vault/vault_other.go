//go:build !windows

package vault

type unsupportedVault struct{}

func newPlatformVault(string) Vault                    { return unsupportedVault{} }
func (unsupportedVault) Put(string, Credentials) error { return ErrUnsupported }
func (unsupportedVault) Get(string) (Credentials, error) {
	return Credentials{}, ErrUnsupported
}
func (unsupportedVault) Delete(string) error { return ErrUnsupported }
