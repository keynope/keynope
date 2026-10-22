package app

// Keep the shared editor shell out of the presentation renderer. Both hosts
// embed the same implementation; exported slides do not mount a workspace.
var workspaceJS = embeddedString("web/workspace.js")

var workspaceCSS = embeddedString("web/workspace.css")

var interfaceCSS = embeddedString("web/interface.css")

var conversionReportJS = embeddedString("web/conversion-report.js")

var templatesJS = embeddedString("web/templates.js")

var tablesJS = embeddedString("web/tables.js")

var chartsJS = embeddedString("web/charts.js")

var buildsJS = transitionCurvesJS() + embeddedString("web/builds.js")

var presentationToolsJS = embeddedString("web/presentation-tools.js")
var videoJS = embeddedString("web/video.js")
