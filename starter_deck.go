package main

import _ "embed"

// bundledStarterDeckMarkdown is shared by the terminal's New flow and the
// Welcome.md resource copied into the Mac application bundle.
//
//go:embed app/Welcome.md
var bundledStarterDeckMarkdown string
