//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Foundation
#include <stddef.h>

void *keynopeStartAccessingBookmark(const void *bytes, size_t length);
void keynopeStopAccessingBookmark(void *handle);
*/
import "C"

import (
	"encoding/base64"
	"fmt"
	"os"
	"unsafe"
)

var sandboxBookmarkHandle unsafe.Pointer

func startSandboxAccessFromEnvironment() error {
	encoded := os.Getenv("KEYNOPE_SANDBOX_BOOKMARK")
	if encoded == "" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode sandbox bookmark: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("sandbox bookmark is empty")
	}
	handle := C.keynopeStartAccessingBookmark(unsafe.Pointer(&data[0]), C.size_t(len(data)))
	if handle == nil {
		return fmt.Errorf("could not resolve sandbox bookmark")
	}
	sandboxBookmarkHandle = handle
	return nil
}

func stopSandboxAccess() {
	if sandboxBookmarkHandle == nil {
		return
	}
	C.keynopeStopAccessingBookmark(sandboxBookmarkHandle)
	sandboxBookmarkHandle = nil
}
