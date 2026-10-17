package app

import (
	"os"
	"path/filepath"
)

var testRepositoryRoot = func() string {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			panic("cannot locate repository test resources")
		}
		root = parent
	}
}()

// Resolve repository-owned fixtures independently of an individual test's
// working directory. Tests that exercise document paths can still use t.Chdir.
func testRepositoryPath(parts ...string) string {
	return filepath.Join(append([]string{testRepositoryRoot}, parts...)...)
}
