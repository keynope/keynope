//go:build !darwin || !cgo

package main

func startSandboxAccessFromEnvironment() error { return nil }
func stopSandboxAccess()                       {}
