package main

// Transfer the attachment geometry already computed by a full scene projection
// to its immediate page projection. This is not a global cache or authored
// metadata: only arrays already emitted in exportPage survive that call.
type sceneAttachmentGeometry struct {
	scene      *slideScene
	cols, rows int
	ports      []shapePort
	connectors []shapeConnector
}

func scenePageAttachments(slide Slide, cols, rows int) ([]shapePort, []shapeConnector) {
	if geometry := slide.previewGeometry; geometry != nil && geometry.scene == slide.ModernScene && geometry.cols == cols && geometry.rows == rows {
		return geometry.ports, geometry.connectors
	}
	// Imported/manual scene projections and changed viewport contexts retain the
	// compatible cold path. Reuse its ports within this pass as well.
	lines := layout(withTrueTypeDefaults(slide), cols, rows)
	ports := slideShapePorts(slide, lines, cols, rows)
	return ports, slideShapeConnectorsWithPorts(slide, lines, cols, rows, ports)
}
