//go:build !darwin

package auth

func withDarwinKeychainLock(fn func() error) error {
	return fn()
}
