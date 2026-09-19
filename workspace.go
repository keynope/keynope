package main

import _ "embed"

// Keep the shared editor shell out of the presentation renderer. Both hosts
// embed the same implementation; exported slides do not mount a workspace.
//
//go:embed web/workspace.js
var workspaceJS string

//go:embed web/workspace.css
var workspaceCSS string
