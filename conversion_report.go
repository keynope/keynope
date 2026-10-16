package main

import "keynope/internal/pptx"

// Runtime notices use value fields only. They are safe to copy with an editor
// snapshot and never serialized as authored Markdown or audience scene data.
func documentConversionDiagnostics(report pptx.Report) []documentDiagnostic {
	var result []documentDiagnostic
	for _, warning := range report.Normalized().Warnings {
		result = append(result, documentDiagnostic{Code: warning.Code, Message: warning.Message, Conversion: true, Slide: warning.Slide, Object: warning.Object, Feature: warning.Feature, Severity: warning.Severity, Fallback: warning.Fallback, Remediation: warning.Remediation})
	}
	return result
}
