package app

// Browser-owned shaping keeps the painted DOM and interaction geometry in one
// coordinate system. The Retro compatibility renderer remains unchanged while
// the semantic Modern scene adapter is built on this boundary.
var shapedTextJS = embeddedString("web/text-layout.js")

var textStyleToolsJS = embeddedString("web/text-styles.js")

var sceneJS = embeddedString("web/scene.js")
