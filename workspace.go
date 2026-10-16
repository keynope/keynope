package main

import _ "embed"

// Keep the shared editor shell out of the presentation renderer. Both hosts
// embed the same implementation; exported slides do not mount a workspace.
//
//go:embed web/workspace.js
var workspaceJS string

//go:embed web/workspace.css
var workspaceCSS string

//go:embed web/interface.css
var interfaceCSS string

//go:embed web/conversion-report.js
var conversionReportJS string

//go:embed web/templates.js
var templatesJS string

//go:embed web/tables.js
var tablesJS string

//go:embed web/charts.js
var chartsJS string

//go:embed web/builds.js
var buildsJS string

//go:embed web/presentation-tools.js
var presentationToolsJS string
