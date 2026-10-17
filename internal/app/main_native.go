//go:build (!js || !wasm) && !keynope_sitegen

package app

func Run() {
	appEngineMain()
}
