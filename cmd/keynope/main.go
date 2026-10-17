// Command keynope is the private application engine. Platform-specific entry
// points in internal/app select native, WebAssembly or offline site generation.
package main

import "keynope/internal/app"

func main() {
	app.Run()
}
