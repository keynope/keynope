package pptx

import "strings"

// Normalized owns its result, preserves encounter order and never merges notes
// across different source slides/objects or different loss descriptions.
func (report Report) Normalized() Report {
	result := Report{}
	seen := map[Warning]bool{}
	for _, warning := range report.Warnings {
		if warning.Feature == "" {
			warning.Feature = warningFeature(warning.Code)
		}
		if warning.Severity == "" {
			warning.Severity = "warning"
			switch warning.Code {
			case "image-animation-embedded", "retro-image-animation-embedded", "image-colour-baked":
				warning.Severity = "info"
			}
		}
		if warning.Fallback == "" {
			warning.Fallback = warningFallback(warning.Code)
		}
		if warning.Remediation == "" {
			warning.Remediation = warningRemediation(warning.Feature, warning.Fallback)
		}
		if !seen[warning] {
			seen[warning] = true
			result.Warnings = append(result.Warnings, warning)
		}
	}
	return result
}

func warningFeature(code string) string {
	switch {
	case strings.HasPrefix(code, "video-"):
		return "video"
	case strings.HasPrefix(code, "table-"):
		return "table"
	case strings.HasPrefix(code, "chart-"):
		return "chart"
	case strings.HasPrefix(code, "image-"), strings.HasPrefix(code, "retro-"), strings.HasPrefix(code, "svg-"), strings.HasPrefix(code, "reflected-svg-"):
		return "image"
	case strings.HasPrefix(code, "shape-"), strings.HasPrefix(code, "custom-shape-"):
		return "shape"
	case strings.HasPrefix(code, "connector-"), strings.HasPrefix(code, "line-"):
		return "connector"
	case strings.HasPrefix(code, "font-"), strings.HasPrefix(code, "word-"), strings.HasPrefix(code, "text-"), strings.HasPrefix(code, "code-container-"), strings.HasPrefix(code, "paragraph-"), strings.HasPrefix(code, "run-font-"), strings.HasPrefix(code, "mixed-list-"):
		return "text"
	case strings.HasPrefix(code, "group-"), code == "inherited-group-flattened":
		return "group"
	case strings.HasPrefix(code, "background-"), code == "animated-background-frozen":
		return "background"
	case strings.HasPrefix(code, "transition-"), strings.HasPrefix(code, "timing-"), strings.HasPrefix(code, "build-"), code == "automatic-slide-advance-omitted":
		return "animation"
	default:
		return "document"
	}
}

// Explicit outcomes, not guesses based on message prose or a code suffix.
// New/foreign codes remain honestly unclassified until their mapping is added.
func warningFallback(code string) string {
	if code == "stroke-position-approximated" || code == "object-effects-omitted" {
		return "partial-conversion"
	}
	switch code {
	case "text-gradient-reader-dependent", "page-size-rounded", "font-not-embedded", "font-embedding-restricted":
		return "partial-conversion"
	case "image-animation-embedded", "retro-image-animation-embedded":
		return "animated-gif"
	case "image-colour-baked":
		return "treated-image"
	case "image-paint-rasterized", "reflected-svg-rasterized", "retro-font-artwork-rasterized", "retro-image-rasterized":
		return "picture-artwork"
	case "video-poster-only", "animated-background-frozen", "image-animation-frozen", "retro-image-animation-frozen":
		return "static-first-frame"
	case "chart-theme-colours-frozen", "chart-theme-font-frozen", "word-theme-colours-frozen", "word-theme-fonts-frozen":
		return "resolved-formatting"
	case "group-flattened", "inherited-group-flattened":
		return "ungrouped-objects"
	case "chart-type-unsupported", "chart-combination-unsupported", "chart-stacked-unsupported", "chart-part-missing", "chart-data-missing", "unsupported-graphic-frame":
		return "unsupported-content"
	case "chart-source-too-large", "chart-source-budget-exceeded":
		return "recovery-data-not-retained"
	case "image-source-missing", "image-source-unsupported", "image-missing", "background-image-unavailable", "svg-fallback-unavailable", "retro-artwork-omitted", "retro-image-capture-unavailable", "connector-missing-points", "line-no-stroke", "table-data-missing", "invalid-bounds", "unsupported-object":
		return "content-omitted"
	case "build-target-omitted", "automatic-slide-advance-omitted", "chart-external-data-ignored", "chart-link-omitted", "table-link-omitted", "link-omitted", "line-arrowheads-omitted", "object-name-omitted", "code-container-omitted", "connector-attachment-omitted", "connector-attachment-unsupported":
		return "feature-omitted"
	case "tab-slide-excluded":
		return "private-tab-excluded"
	case "hidden-slide-visible":
		return "ordinary-visible-slide"
	case "svg-source-refused":
		return "validated-raster-fallback"
	case "image-animation-alpha-approximated", "image-animation-colours-quantized", "image-animation-timing-rounded":
		return "gif-approximation"
	case "chart-label-style-approximated", "text-size-clamped", "paragraph-spacing-clamped", "paragraph-style-approximated", "mixed-list-paragraphs-approximated", "run-font-size-clamped":
		return "partial-conversion"
	case "background-colour-unreadable", "background-image-unreadable", "chart-effects-approximated", "chart-font-width-approximated", "chart-option-approximated", "chart-secondary-axes-approximated", "connector-arrow-size-approximated", "connector-attachment-approximated", "connector-port-approximated", "connector-width-clamped", "custom-shape-geometry-approximated", "custom-shape-geometry-unsupported", "font-width-approximated", "group-reflection-approximated", "group-shadow-approximated", "group-skew-approximated", "group-text-width-approximated", "image-colour-unavailable", "image-destination-approximated", "image-paint-approximated", "image-shadow-approximated", "line-route-approximated", "shape-adjustment-formula-unsupported", "shape-adjustments-approximated", "shape-geometry-approximated", "shape-label-effects-approximated", "shape-label-width-approximated", "shape-paint-approximated", "shape-shadow-approximated", "shape-shadow-opacity-approximated", "shape-stroke-width-clamped", "table-border-segments-reader-dependent", "table-effects-approximated", "table-font-width-approximated", "text-effects-approximated", "timing-unsupported", "transition-duration-estimated", "transition-fallback":
		return "partial-conversion"
	default:
		return "unclassified"
	}
}

func warningRemediation(feature, fallback string) string {
	switch fallback {
	case "private-tab-excluded":
		return "Keep participant-only pages in the Keynope deck; make a public slide copy only if it should be included."
	case "animated-gif", "gif-approximation", "static-first-frame":
		return "Check playback in the intended viewer. Use the HTML presentation when full animation fidelity is required."
	case "picture-artwork", "treated-image":
		return "Edit the source image and its treatments in Keynope, then export again. Keep the Markdown deck as the editable original."
	case "content-omitted", "unsupported-content", "recovery-data-not-retained":
		return "Review the affected slide against the original. Recreate unsupported content using supported objects or use exact-appearance PDF/HTML."
	case "validated-raster-fallback":
		return "Use the retained raster image, or replace the source with a supported passive SVG."
	case "resolved-formatting":
		return "Check the current font/colour in the target viewer; theme-linked changes may need to be made in Keynope and exported again."
	default:
		if feature == "animation" {
			return "Check build order and transitions in the intended viewer, or present the HTML version."
		}
		return "Review the affected object in the intended viewer. Keep the original deck; use exact-appearance PDF/HTML if the conversion is not suitable."
	}
}
