//go:build (!js || !wasm) && !keynope_sitegen

package main

func main() {
	appEngineMain()
}
