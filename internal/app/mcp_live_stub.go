//go:build js || keynope_sitegen

package app

import "net/http"

func configureLiveEditorMCP(*http.ServeMux, *nativeEditorSession, *presenterCompanion) {}
func liveMCPReadPath(string) bool                                                      { return false }
