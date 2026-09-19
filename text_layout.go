package main

import _ "embed"

// Browser-owned shaping keeps the painted DOM and interaction geometry in one
// coordinate system. The Retro compatibility renderer remains unchanged while
// the semantic Modern scene adapter is built on this boundary.
//
//go:embed web/text-layout.js
var shapedTextJS string

//go:embed web/scene.js
var sceneJS string
