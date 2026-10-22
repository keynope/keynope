//go:build (!js || !wasm) && !keynope_sitegen

package app

import (
	"fmt"
	"os"
)

func Run() {
	if len(os.Args) > 1 && os.Args[1] == "--mcp-http" {
		if err := runMCPHTTP(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--mcp" {
		if err := runMCP(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	appEngineMain()
}
