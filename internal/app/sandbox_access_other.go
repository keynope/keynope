//go:build !darwin || !cgo

package app

func startSandboxAccessFromEnvironment() error { return nil }
func stopSandboxAccess()                       {}
