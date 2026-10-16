package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"regexp"
	"strings"
)

func validateDeckObjectReferences(deck Deck) error {
	return validateObjectReferenceScopes(deck)
}

// Validate the entire selection before allocating or changing a payload. This
// lets duplication/paste remain atomic even if a source object is incomplete.
func duplicateStructuredReferences(deck *Deck, elements []Element) error {
	for _, element := range elements {
		if element.ObjectID == "" {
			continue
		}
		if deck.Objects == nil {
			return fmt.Errorf("missing structured object archive")
		}
		switch element.Kind {
		case "table":
			if _, ok := deck.Objects.Tables[element.ObjectID]; !ok {
				return fmt.Errorf("missing table payload")
			}
		case "chart":
			if _, ok := deck.Objects.Charts[element.ObjectID]; !ok {
				return fmt.Errorf("missing chart payload")
			}
		default:
			return fmt.Errorf("cannot duplicate unknown structured object kind %q", element.Kind)
		}
	}
	for i := range elements {
		element := &elements[i]
		if element.ObjectID == "" {
			continue
		}
		id := newStableID(element.Kind)
		if element.Kind == "table" {
			deck.Objects.Tables[id] = cloneTable(deck.Objects.Tables[element.ObjectID])
		}
		if element.Kind == "chart" {
			deck.Objects.Charts[id] = cloneChart(deck.Objects.Charts[element.ObjectID])
		}
		element.ObjectID = id
	}
	return nil
}

// Reidentify a slide and its build targets as one operation. Connection
// endpoints are remapped by the existing connector copy implementation.
func cloneStructuredSlide(deck *Deck, source Slide, destination *Slide) error {
	if err := duplicateStructuredReferences(deck, destination.Elements); err != nil {
		return err
	}
	ids := map[string]string{}
	oldIDs := make([]string, len(destination.Elements))
	for i, e := range source.Elements {
		if i < len(oldIDs) {
			oldIDs[i] = e.ID
		}
	}
	freshPastedGroups(destination.Elements)
	freshPastedConnectorIDs(destination.Elements)
	for i, e := range destination.Elements {
		if oldIDs[i] != "" {
			ids[oldIDs[i]] = e.ID
		}
	}
	if media := destination.BackgroundMedia; media != nil && media.SourceObjectID != "" {
		// The background points into this newly copied scope, not the source
		// page's hidden owner. Missing advisory owners remain source-only media.
		media.SourceObjectID = ids[media.SourceObjectID]
	}
	// A copy is a newly authored page, including when its legacy source has
	// no ID. Never reuse the source identity or let an empty copy vanish on save.
	destination.ID = newStableID("slide")
	destination.notesOwnerID = ""
	if source.ID != "" {
		if deck.Objects != nil {
			if build, ok := deck.Objects.Builds[source.ID]; ok {
				build = cloneBuild(build)
				for i := range build.Steps {
					for j, id := range build.Steps[i].TargetIDs {
						if fresh := ids[id]; fresh != "" {
							build.Steps[i].TargetIDs[j] = fresh
						}
					}
				}
				deck.Objects.Builds[destination.ID] = build
			}
		}
	}
	return nil
}

const structuredObjectsVersion = 1
const maxStructuredObjectBytes = 16 << 20

// DeckObjects is authored data. A renderer consumes only the payload referenced
// by its visible slide, never this archive wholesale.
type DeckObjects struct {
	Version int                   `json:"version"`
	Tables  map[string]TableData  `json:"tables,omitempty"`
	Charts  map[string]ChartData  `json:"charts,omitempty"`
	Builds  map[string]SlideBuild `json:"builds,omitempty"`
	Extra   *jsonExtensions       `json:"-"`
}
type TableData struct {
	Version int             `json:"version"`
	Rows    []TableRow      `json:"rows"`
	Columns []TableColumn   `json:"columns"`
	Cells   [][]TableCell   `json:"cells"`
	Style   TableStyle      `json:"style,omitempty"`
	Extra   *jsonExtensions `json:"-"`
}
type TableRow struct {
	Height float64         `json:"height"`
	Extra  *jsonExtensions `json:"-"`
}
type TableColumn struct {
	Width float64         `json:"width"`
	Extra *jsonExtensions `json:"-"`
}
type TableCell struct {
	Text    string          `json:"text,omitempty"`
	Runs    []ObjectTextRun `json:"runs,omitempty"`
	RowSpan int             `json:"rowSpan,omitempty"`
	ColSpan int             `json:"colSpan,omitempty"`
	Hidden  bool            `json:"hidden,omitempty"`
	Style   *TableCellStyle `json:"style,omitempty"`
	// Sparse per-track perimeter paint, relative to this cell's span. Empty
	// segments inherit the side style; covered cells never own fragments.
	BorderFragments map[string][]TableBorderStyle `json:"borderFragments,omitempty"`
	Extra           *jsonExtensions               `json:"-"`
}
type ObjectTextRun struct {
	Text       string          `json:"text"`
	Bold       bool            `json:"bold,omitempty"`
	BoldOff    bool            `json:"boldOff,omitempty"`
	Italic     bool            `json:"italic,omitempty"`
	Underline  bool            `json:"underline,omitempty"`
	Color      string          `json:"color,omitempty"`
	ColorToken string          `json:"colorToken,omitempty"` // Semantic role; Color is the portable RGB fallback.
	FontID     string          `json:"fontId,omitempty"`
	FontRole   string          `json:"fontRole,omitempty"`
	FontSize   float64         `json:"fontSize,omitempty"` // Zero inherits the cell's font size.
	Extra      *jsonExtensions `json:"-"`
}
type TableStyle struct {
	Header     bool            `json:"header,omitempty"`
	Banded     bool            `json:"banded,omitempty"`
	Cell       TableCellStyle  `json:"cell,omitempty"`
	HeaderFill string          `json:"headerFill,omitempty"`
	BandFill   string          `json:"bandFill,omitempty"`
	Extra      *jsonExtensions `json:"-"`
}
type TableCellStyle struct {
	DefaultColor string                      `json:"defaultColor,omitempty"` // Concrete fallback only when neither cell nor owner/slide supplies text colour.
	Fill         string                      `json:"fill,omitempty"`
	Color        string                      `json:"color,omitempty"`
	BorderColor  string                      `json:"borderColor,omitempty"`
	BorderWidth  *float64                    `json:"borderWidth,omitempty"`
	Borders      map[string]TableBorderStyle `json:"borders,omitempty"` // Optional per-side overrides.
	FontID       string                      `json:"fontId,omitempty"`
	FontSize     *float64                    `json:"fontSize,omitempty"`
	FontWidth    *float64                    `json:"fontWidth,omitempty"`
	LineHeight   *float64                    `json:"lineHeight,omitempty"`
	Align        string                      `json:"align,omitempty"`
	Vertical     string                      `json:"vertical,omitempty"`
	Padding      *float64                    `json:"padding,omitempty"`
	Extra        *jsonExtensions             `json:"-"`
}
type TableBorderStyle struct {
	Color string          `json:"color,omitempty"`
	Width *float64        `json:"width,omitempty"`
	Extra *jsonExtensions `json:"-"`
}
type ChartData struct {
	Version        int               `json:"version"`
	Type           string            `json:"type"`
	Title          string            `json:"title,omitempty"`
	Categories     []string          `json:"categories"`
	CategoryColors []ChartColorStyle `json:"categoryColors,omitempty"` // Sparse point colours; used by circular charts.
	Series         []ChartSeries     `json:"series"`
	Options        ChartOptions      `json:"options,omitempty"`
	Extra          *jsonExtensions   `json:"-"`
}
type ChartSeries struct {
	LabelColor string          `json:"labelColor,omitempty"` // RGB, None or theme:<role>; empty chooses accessible contrast automatically.
	Name       string          `json:"name"`
	Values     []float64       `json:"values"`
	Color      string          `json:"color,omitempty"`
	ColorToken string          `json:"colorToken,omitempty"` // Role plus portable RGB fallback in Color.
	Extra      *jsonExtensions `json:"-"`
}
type ChartColorStyle struct {
	LabelColor string          `json:"labelColor,omitempty"`
	Color      string          `json:"color,omitempty"`
	ColorToken string          `json:"colorToken,omitempty"`
	Extra      *jsonExtensions `json:"-"`
}
type ChartOptions struct {
	DefaultColor  string          `json:"defaultColor,omitempty"`  // Concrete fallback; explicit owner/slide foreground wins.
	DefaultFontID string          `json:"defaultFontId,omitempty"` // Concrete fallback; explicit owner font wins.
	FontID        string          `json:"fontId,omitempty"`
	FontRole      string          `json:"fontRole,omitempty"` // Family-only theme role; FontID is its portable concrete fallback.
	FontWidth     *float64        `json:"fontWidth,omitempty"`
	GridColor     string          `json:"gridColor,omitempty"`
	HideAxes      bool            `json:"hideAxes,omitempty"`
	Legend        bool            `json:"legend,omitempty"`
	DataLabels    bool            `json:"dataLabels,omitempty"`
	Stacked       bool            `json:"stacked,omitempty"`
	ValueFormat   string          `json:"valueFormat,omitempty"`
	XTitle        string          `json:"xTitle,omitempty"`
	YTitle        string          `json:"yTitle,omitempty"`
	Minimum       *float64        `json:"minimum,omitempty"`
	Maximum       *float64        `json:"maximum,omitempty"`
	Extra         *jsonExtensions `json:"-"`
}
type SlideBuild struct {
	Version    int             `json:"version"`
	Transition SlideTransition `json:"transition,omitempty"`
	Steps      []BuildStep     `json:"steps,omitempty"`
	Extra      *jsonExtensions `json:"-"`
}
type SlideTransition struct {
	Effect     string          `json:"effect,omitempty"`
	Direction  string          `json:"direction,omitempty"`
	DurationMS int             `json:"durationMs,omitempty"`
	Extra      *jsonExtensions `json:"-"`
}
type BuildStep struct {
	TargetIDs  []string        `json:"targetIds"`
	Effect     string          `json:"effect"`
	Direction  string          `json:"direction,omitempty"`
	DurationMS int             `json:"durationMs,omitempty"`
	Advance    string          `json:"advance,omitempty"`
	DelayMS    int             `json:"delayMs,omitempty"`
	Extra      *jsonExtensions `json:"-"`
}

var structuredObjectsRE = regexp.MustCompile(`(?m)^<!--\s*keynope-objects\s+version=([0-9]+)\s+base64:([A-Za-z0-9+/=]+)\s*-->[\t ]*\r?\n?`)
var structuredObjectsEnvelopeRE = metadataNamespaceEnvelopeRE("keynope-objects")
var slideIdentityRE = regexp.MustCompile(`^<!--\s*keynope-slide-id=([A-Za-z0-9_-]{1,100})\s*-->`)

func decodeDeckObjects(text string) (*DeckObjects, string, error) {
	matches := findDocumentMetadata(text, structuredObjectsRE, 2)
	if len(matches) == 0 {
		if firstDocumentMetadata(text, structuredObjectsEnvelopeRE) != nil {
			return nil, text, fmt.Errorf("malformed structured object metadata")
		}
		return nil, text, nil
	}
	if len(matches) != 1 || len(findDocumentMetadataIndices(text, structuredObjectsEnvelopeRE, 2)) != 1 {
		return nil, text, fmt.Errorf("ambiguous structured object metadata")
	}
	match := matches[0]
	if match[1] != "1" || len(match[2]) > maxStructuredObjectBytes*2 {
		return nil, text, fmt.Errorf("unsupported or oversized structured object metadata")
	}
	compressed, err := base64.StdEncoding.DecodeString(match[2])
	if err != nil {
		return nil, text, fmt.Errorf("decode structured objects: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, text, fmt.Errorf("open structured objects: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, maxStructuredObjectBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(data) > maxStructuredObjectBytes {
		return nil, text, fmt.Errorf("structured objects are invalid or exceed %d bytes", maxStructuredObjectBytes)
	}
	var objects DeckObjects
	if err := json.Unmarshal(data, &objects); err != nil {
		return nil, text, fmt.Errorf("parse structured objects: %w", err)
	}
	if err := objects.Validate(); err != nil {
		return nil, text, err
	}
	return &objects, rewriteDocumentMetadata(text, structuredObjectsRE, nil), nil
}

func encodeDeckObjects(objects *DeckObjects) (string, error) {
	if objects == nil {
		return "", nil
	}
	if err := objects.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(objects)
	if err != nil || len(data) > maxStructuredObjectBytes {
		return "", fmt.Errorf("structured objects are invalid or too large")
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return "<!-- keynope-objects version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->", nil
}

func (objects *DeckObjects) Validate() error {
	if objects == nil {
		return nil
	}
	if objects.Version != structuredObjectsVersion {
		return fmt.Errorf("unsupported structured objects version %d", objects.Version)
	}
	if len(objects.Tables)+len(objects.Charts)+len(objects.Builds) > 10000 {
		return fmt.Errorf("too many structured objects")
	}
	seen := map[string]bool{}
	usage := structuredBudgetUsage{Objects: int64(len(objects.Tables) + len(objects.Charts) + len(objects.Builds))}
	for id, table := range objects.Tables {
		if !validObjectID(id) || seen[id] {
			return fmt.Errorf("invalid or duplicate table ID %q", id)
		}
		seen[id] = true
		if err := table.Validate(); err != nil {
			return fmt.Errorf("table %s: %w", id, err)
		}
		usage = usage.add(tableBudgetUsage(table))
	}
	for id, chart := range objects.Charts {
		if !validObjectID(id) || seen[id] {
			return fmt.Errorf("invalid or duplicate chart ID %q", id)
		}
		seen[id] = true
		if err := chart.Validate(); err != nil {
			return fmt.Errorf("chart %s: %w", id, err)
		}
		usage = usage.add(chartBudgetUsage(chart))
	}
	for id, build := range objects.Builds {
		if !validObjectID(id) {
			return fmt.Errorf("invalid build slide ID %q", id)
		}
		if err := build.Validate(); err != nil {
			return fmt.Errorf("slide build %s: %w", id, err)
		}
		usage.EncodedBytes += structuredPayloadBytes(build)
	}
	usage.EncodedBytes += structuredRegistryOverhead(objects, structuredRegistryHeaderBytes(objects))
	return usage.validate(defaultStructuredBudgetLimits)
}
func validObjectID(id string) bool {
	return id != "" && len(id) <= 100 && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) < 0
}
func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (table TableData) Validate() error {
	if table.Version != 1 {
		return fmt.Errorf("unsupported table version %d", table.Version)
	}
	rows, cols := len(table.Rows), len(table.Columns)
	if rows < 1 || cols < 1 || rows > 100 || cols > 100 || len(table.Cells) != rows {
		return fmt.Errorf("table must contain 1–100 rows and columns")
	}
	for _, row := range table.Rows {
		if !finitePositive(row.Height) {
			return fmt.Errorf("row heights must be positive finite weights")
		}
	}
	for _, col := range table.Columns {
		if !finitePositive(col.Width) {
			return fmt.Errorf("column widths must be positive finite weights")
		}
	}
	covered := make([][]bool, rows)
	for r := range covered {
		if len(table.Cells[r]) != cols {
			return fmt.Errorf("cell grid does not match table dimensions")
		}
		covered[r] = make([]bool, cols)
	}
	if err := table.Style.Cell.Validate(); err != nil {
		return err
	}
	for _, color := range []string{table.Style.HeaderFill, table.Style.BandFill} {
		if !validTableColor(color) {
			return fmt.Errorf("invalid table color")
		}
	}
	textBytes := 0
	for r, cells := range table.Cells {
		for c, cell := range cells {
			if cell.Style != nil {
				if err := cell.Style.Validate(); err != nil {
					return err
				}
			}
			if cell.Hidden {
				if !covered[r][c] || cell.Text != "" || len(cell.Runs) != 0 || cell.RowSpan != 0 || cell.ColSpan != 0 || len(cell.BorderFragments) != 0 {
					return fmt.Errorf("invalid covered cell at row %d, column %d", r+1, c+1)
				}
				continue
			}
			if covered[r][c] || cell.RowSpan < 0 || cell.ColSpan < 0 {
				return fmt.Errorf("overlapping or invalid cell span")
			}
			height, width := max(1, cell.RowSpan), max(1, cell.ColSpan)
			if r+height > rows || c+width > cols {
				return fmt.Errorf("cell span exceeds table bounds")
			}
			if len(cell.BorderFragments) > 4 {
				return fmt.Errorf("too many table perimeter sides")
			}
			for side, fragments := range cell.BorderFragments {
				length := width
				if side == "left" || side == "right" {
					length = height
				}
				if len(fragments) != length {
					return fmt.Errorf("table perimeter fragment count does not match span")
				}
				for _, border := range fragments {
					if err := (TableCellStyle{Borders: map[string]TableBorderStyle{side: border}}).Validate(); err != nil {
						return err
					}
				}
			}
			for y := r; y < r+height; y++ {
				for x := c; x < c+width; x++ {
					if covered[y][x] {
						return fmt.Errorf("overlapping cell spans")
					}
					covered[y][x] = true
					if (y != r || x != c) && !table.Cells[y][x].Hidden {
						return fmt.Errorf("merged cell must cover hidden cells")
					}
				}
			}
			textBytes += len(cell.Text)
			for _, run := range cell.Runs {
				if run.Bold && run.BoldOff {
					return fmt.Errorf("conflicting text run bold overrides")
				}
				textBytes += len(run.Text)
				if !validObjectColor(run.Color) {
					return fmt.Errorf("invalid text run color")
				}
				if run.ColorToken != "" && (!validThemeColorToken(run.ColorToken) || len(run.Color) != 7 || !strings.HasPrefix(run.Color, "#")) {
					return fmt.Errorf("invalid text run colour role or RGB fallback")
				}
				if run.FontID != "" && run.FontID != "c64" && run.FontID != "sans" && run.FontID != "mono" {
					return fmt.Errorf("invalid text run font")
				}
				if run.FontRole != "" && (!validThemeFontRole(run.FontRole) || !validBundledFont(run.FontID)) {
					return fmt.Errorf("invalid text run font role or fallback")
				}
				if run.FontSize < 0 || run.FontSize > 1024 || math.IsNaN(run.FontSize) || math.IsInf(run.FontSize, 0) {
					return fmt.Errorf("invalid text run font size")
				}
			}
			if textBytes > 1<<20 {
				return fmt.Errorf("table text exceeds 1 MiB")
			}
		}
	}
	return nil
}
func validObjectColor(color string) bool {
	if color == "" || color == "none" {
		return true
	}
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	return strings.IndexFunc(color[1:], func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') }) < 0
}
func (style TableCellStyle) Validate() error {
	if style.DefaultColor != "" && (len(style.DefaultColor) != 7 || !validObjectColor(style.DefaultColor)) {
		return fmt.Errorf("invalid cell default text colour")
	}
	if len(style.Borders) > 4 {
		return fmt.Errorf("table cell has too many border sides")
	}
	for side, border := range style.Borders {
		if side != "left" && side != "right" && side != "top" && side != "bottom" {
			return fmt.Errorf("invalid table border side")
		}
		if !validTableColor(border.Color) || border.Width != nil && (*border.Width < 0 || *border.Width > 100 || math.IsNaN(*border.Width) || math.IsInf(*border.Width, 0)) {
			return fmt.Errorf("invalid table edge border")
		}
	}
	for _, color := range []string{style.Fill, style.Color, style.BorderColor} {
		if !validTableColor(color) {
			return fmt.Errorf("invalid cell color")
		}
	}
	if style.Align != "" && style.Align != "left" && style.Align != "center" && style.Align != "right" && style.Align != "justify" {
		return fmt.Errorf("invalid cell alignment")
	}
	if style.Vertical != "" && style.Vertical != "top" && style.Vertical != "middle" && style.Vertical != "bottom" {
		return fmt.Errorf("invalid cell vertical alignment")
	}
	for _, value := range []*float64{style.BorderWidth, style.Padding} {
		if value != nil && (*value < 0 || *value > 100 || math.IsNaN(*value) || math.IsInf(*value, 0)) {
			return fmt.Errorf("invalid cell border/padding")
		}
	}
	if style.FontSize != nil && (!finitePositive(*style.FontSize) || *style.FontSize > 1024) {
		return fmt.Errorf("invalid cell font size")
	}
	for name, value := range map[string]*float64{"fontWidth": style.FontWidth, "lineHeight": style.LineHeight} {
		low, high, _ := tableCellMetricRange(name)
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < low || *value > high) {
			return fmt.Errorf("invalid cell %s", name)
		}
	}
	if style.FontID != "" && !validObjectID(style.FontID) {
		return fmt.Errorf("invalid cell font ID")
	}
	return nil
}
func (chart ChartData) Validate() error {
	if chart.Version != 1 {
		return fmt.Errorf("unsupported chart version %d", chart.Version)
	}
	switch chart.Type {
	case "column", "bar", "line", "area", "pie", "doughnut":
	default:
		return fmt.Errorf("unsupported chart type %q", chart.Type)
	}
	if len(chart.Categories) < 1 || len(chart.Categories) > 2000 || len(chart.Series) < 1 || len(chart.Series) > 64 {
		return fmt.Errorf("chart needs 1–2000 categories and 1–64 series")
	}
	circular := chart.Type == "pie" || chart.Type == "doughnut"
	if circular && len(chart.Series) != 1 {
		return fmt.Errorf("pie/doughnut charts require one series")
	}
	if chart.Options.Stacked && chart.Type != "column" && chart.Type != "bar" {
		return fmt.Errorf("stacking requires a column or bar chart")
	}
	if chart.Options.ValueFormat != "" && chart.Options.ValueFormat != "number" && chart.Options.ValueFormat != "percent" {
		return fmt.Errorf("unsupported chart number format")
	}
	if chart.Options.FontID != "" && !validBundledFont(chart.Options.FontID) {
		return fmt.Errorf("invalid chart font")
	}
	if chart.Options.FontRole != "" && (!validThemeFontRole(chart.Options.FontRole) || !validBundledFont(chart.Options.FontID)) {
		return fmt.Errorf("invalid chart font role or fallback")
	}
	if chart.Options.DefaultFontID != "" && !validBundledFont(chart.Options.DefaultFontID) {
		return fmt.Errorf("invalid chart default font")
	}
	if chart.Options.DefaultColor != "" && (len(chart.Options.DefaultColor) != 7 || !validObjectColor(chart.Options.DefaultColor)) {
		return fmt.Errorf("invalid chart default text colour")
	}
	if chart.Options.FontWidth != nil && (math.IsNaN(*chart.Options.FontWidth) || math.IsInf(*chart.Options.FontWidth, 0) || *chart.Options.FontWidth < 1 || *chart.Options.FontWidth > 200) {
		return fmt.Errorf("invalid chart font width")
	}
	if !validTableColor(chart.Options.GridColor) {
		return fmt.Errorf("invalid chart grid colour")
	}
	textBytes := len(chart.Title) + len(chart.Options.XTitle) + len(chart.Options.YTitle)
	for _, category := range chart.Categories {
		textBytes += len(category)
	}
	for _, series := range chart.Series {
		textBytes += len(series.Name)
		if len(series.Values) != len(chart.Categories) {
			return fmt.Errorf("series length does not match categories")
		}
		if !validChartColorStyle(ChartColorStyle{Color: series.Color, ColorToken: series.ColorToken, LabelColor: series.LabelColor}) {
			return fmt.Errorf("invalid series color")
		}
		for _, value := range series.Values {
			if math.IsNaN(value) || math.IsInf(value, 0) || circular && value < 0 {
				return fmt.Errorf("invalid chart value")
			}
		}
	}
	if len(chart.CategoryColors) > len(chart.Categories) {
		return fmt.Errorf("too many chart category colours")
	}
	for _, style := range chart.CategoryColors {
		if !validChartColorStyle(style) {
			return fmt.Errorf("invalid chart category colour")
		}
	}
	if textBytes > 1<<20 {
		return fmt.Errorf("chart text exceeds 1 MiB")
	}
	for _, value := range []*float64{chart.Options.Minimum, chart.Options.Maximum} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
			return fmt.Errorf("invalid axis bound")
		}
	}
	if chart.Options.Minimum != nil && chart.Options.Maximum != nil && *chart.Options.Minimum >= *chart.Options.Maximum {
		return fmt.Errorf("axis minimum must be less than maximum")
	}
	return nil
}
func validBuildDirection(direction string) bool {
	return direction == "" || direction == "left" || direction == "right" || direction == "up" || direction == "down"
}
func (build SlideBuild) Validate() error {
	if build.Version != 1 {
		return fmt.Errorf("unsupported build version %d", build.Version)
	}
	switch build.Transition.Effect {
	case "", "cut", "fade", "push", "wipe":
	default:
		return fmt.Errorf("unsupported transition")
	}
	if !validBuildDirection(build.Transition.Direction) || build.Transition.DurationMS < 0 || build.Transition.DurationMS > 60000 {
		return fmt.Errorf("invalid transition settings")
	}
	if len(build.Steps) > 1000 {
		return fmt.Errorf("too many build steps")
	}
	for _, step := range build.Steps {
		switch step.Effect {
		case "appear", "fade", "wipe", "zoom":
		default:
			return fmt.Errorf("unsupported build effect")
		}
		if len(step.TargetIDs) < 1 || len(step.TargetIDs) > 1000 || !validBuildDirection(step.Direction) || step.DurationMS < 0 || step.DurationMS > 60000 || step.DelayMS < 0 || step.DelayMS > 3600000 {
			return fmt.Errorf("invalid build step")
		}
		if step.Advance != "" && step.Advance != "click" && step.Advance != "after" {
			return fmt.Errorf("invalid build advance")
		}
		seen := map[string]bool{}
		for _, id := range step.TargetIDs {
			if !validObjectID(id) || seen[id] {
				return fmt.Errorf("invalid or duplicate build target")
			}
			seen[id] = true
		}
	}
	return nil
}

func cloneCellStyle(style TableCellStyle) TableCellStyle {
	style.Extra = cloneJSONExtensions(style.Extra)
	if style.Borders != nil {
		borders := make(map[string]TableBorderStyle, len(style.Borders))
		for side, border := range style.Borders {
			border.Extra = cloneJSONExtensions(border.Extra)
			if border.Width != nil {
				width := *border.Width
				border.Width = &width
			}
			borders[side] = border
		}
		style.Borders = borders
	}
	for _, field := range []**float64{&style.BorderWidth, &style.FontSize, &style.FontWidth, &style.LineHeight, &style.Padding} {
		if *field != nil {
			value := **field
			*field = &value
		}
	}
	return style
}
func cloneTable(table TableData) TableData {
	table.Extra = cloneJSONExtensions(table.Extra)
	if table.Rows != nil {
		table.Rows = append([]TableRow{}, table.Rows...)
	}
	for i := range table.Rows {
		table.Rows[i].Extra = cloneJSONExtensions(table.Rows[i].Extra)
	}
	if table.Columns != nil {
		table.Columns = append([]TableColumn{}, table.Columns...)
	}
	for i := range table.Columns {
		table.Columns[i].Extra = cloneJSONExtensions(table.Columns[i].Extra)
	}
	if table.Cells != nil {
		cells := make([][]TableCell, len(table.Cells))
		for r, row := range table.Cells {
			if row == nil {
				continue
			}
			cells[r] = append([]TableCell{}, row...)
			for c := range cells[r] {
				cell := &cells[r][c]
				cell.Extra = cloneJSONExtensions(cell.Extra)
				cell.BorderFragments = cloneTableBorderFragments(cell.BorderFragments)
				if cell.Runs != nil {
					cell.Runs = append([]ObjectTextRun{}, cell.Runs...)
				}
				for i := range cell.Runs {
					cell.Runs[i].Extra = cloneJSONExtensions(cell.Runs[i].Extra)
				}
				if cell.Style != nil {
					style := cloneCellStyle(*cell.Style)
					cell.Style = &style
				}
			}
		}
		table.Cells = cells
	}
	table.Style.Extra = cloneJSONExtensions(table.Style.Extra)
	table.Style.Cell = cloneCellStyle(table.Style.Cell)
	return table
}
func cloneChart(chart ChartData) ChartData {
	chart.Extra = cloneJSONExtensions(chart.Extra)
	if chart.CategoryColors != nil {
		chart.CategoryColors = append([]ChartColorStyle{}, chart.CategoryColors...)
		for i := range chart.CategoryColors {
			chart.CategoryColors[i].Extra = cloneJSONExtensions(chart.CategoryColors[i].Extra)
		}
	}
	if chart.Categories != nil {
		chart.Categories = append([]string{}, chart.Categories...)
	}
	if chart.Series != nil {
		chart.Series = append([]ChartSeries{}, chart.Series...)
	}
	for i := range chart.Series {
		if chart.Series[i].Values != nil {
			chart.Series[i].Values = append([]float64{}, chart.Series[i].Values...)
		}
		chart.Series[i].Extra = cloneJSONExtensions(chart.Series[i].Extra)
	}
	chart.Options.Extra = cloneJSONExtensions(chart.Options.Extra)
	if chart.Options.FontWidth != nil {
		value := *chart.Options.FontWidth
		chart.Options.FontWidth = &value
	}
	if chart.Options.Minimum != nil {
		value := *chart.Options.Minimum
		chart.Options.Minimum = &value
	}
	if chart.Options.Maximum != nil {
		value := *chart.Options.Maximum
		chart.Options.Maximum = &value
	}
	return chart
}
func cloneBuild(build SlideBuild) SlideBuild {
	build.Extra = cloneJSONExtensions(build.Extra)
	build.Transition.Extra = cloneJSONExtensions(build.Transition.Extra)
	if build.Steps != nil {
		build.Steps = append([]BuildStep{}, build.Steps...)
	}
	for i := range build.Steps {
		if build.Steps[i].TargetIDs != nil {
			build.Steps[i].TargetIDs = append([]string{}, build.Steps[i].TargetIDs...)
		}
		build.Steps[i].Extra = cloneJSONExtensions(build.Steps[i].Extra)
	}
	return build
}

// Own the registry containers for commands that replace whole payloads. Values
// not being edited remain borrowed/read-only; the command must clone the target
// before modifying/replacing it. Never use this as a general deep-clone API or
// expose the borrowed values to a mutable draft. Mutable render/input ownership
// still uses Clone(). History replaces every borrowed entry with its own deep
// copy or a private immutable snapshot value before publishing the container.
func (objects *DeckObjects) cloneForPayloadReplacement() *DeckObjects {
	if objects == nil {
		return nil
	}
	copy := *objects
	copy.Extra = cloneJSONExtensions(objects.Extra)
	copy.Tables = maps.Clone(objects.Tables)
	copy.Charts = maps.Clone(objects.Charts)
	copy.Builds = maps.Clone(objects.Builds)
	return &copy
}

func (objects *DeckObjects) Clone() *DeckObjects {
	if objects == nil {
		return nil
	}
	copy := *objects
	copy.Extra = cloneJSONExtensions(objects.Extra)
	if objects.Tables != nil {
		copy.Tables = make(map[string]TableData, len(objects.Tables))
		for id, table := range objects.Tables {
			copy.Tables[id] = cloneTable(table)
		}
	}
	if objects.Charts != nil {
		copy.Charts = make(map[string]ChartData, len(objects.Charts))
		for id, chart := range objects.Charts {
			copy.Charts[id] = cloneChart(chart)
		}
	}
	if objects.Builds != nil {
		copy.Builds = make(map[string]SlideBuild, len(objects.Builds))
		for id, build := range objects.Builds {
			copy.Builds[id] = cloneBuild(build)
		}
	}
	return &copy
}

// Work on a save snapshot only. Undo/history still own all their payloads.
func pruneUnusedDeckObjects(deck *Deck) {
	if deck.Objects == nil || deck.Objects.Extra != nil || hasOpaqueObjectMetadata(*deck) {
		return
	}
	objects, slides := map[string]bool{}, map[string]bool{}
	visit := func(slide Slide) {
		if slide.ID != "" {
			slides[slide.ID] = true
		}
		for _, element := range slide.Elements {
			if element.ObjectID != "" {
				objects[element.ObjectID] = true
			}
		}
	}
	for _, slide := range deck.Slides {
		visit(slide)
	}
	visit(deck.Masters.Base.Slide)
	for _, master := range deck.Masters.Layouts {
		visit(master.Slide)
	}
	for id, data := range deck.Objects.Tables {
		if !objects[id] && data.Extra == nil {
			delete(deck.Objects.Tables, id)
		}
	}
	for id, data := range deck.Objects.Charts {
		if !objects[id] && data.Extra == nil {
			delete(deck.Objects.Charts, id)
		}
	}
	for id, data := range deck.Objects.Builds {
		if !slides[id] && data.Extra == nil {
			delete(deck.Objects.Builds, id)
		}
	}
	if len(deck.Objects.Tables)+len(deck.Objects.Charts)+len(deck.Objects.Builds) == 0 && deck.Objects.Extra == nil {
		deck.Objects = nil
	}
}

func (value DeckObjects) MarshalJSON() ([]byte, error) {
	type stored DeckObjects
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"version", "tables", "charts", "builds"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *DeckObjects) UnmarshalJSON(data []byte) error {
	fields, err := decodeMetadataJSONObject(data, "version", "tables", "charts", "builds")
	if err != nil {
		return err
	}
	for _, namespace := range []string{"tables", "charts", "builds"} {
		if raw, ok := fields[namespace]; ok {
			if _, err := decodeMetadataJSONObject(raw); err != nil {
				return fmt.Errorf("%s registry: %w", namespace, err)
			}
		}
	}
	type stored DeckObjects
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "version", "tables", "charts", "builds")
	if err != nil {
		return err
	}
	*value = DeckObjects(decoded)
	value.Extra = extra
	return nil
}

func (value TableData) MarshalJSON() ([]byte, error) {
	type stored TableData
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"version", "rows", "columns", "cells", "style"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *TableData) UnmarshalJSON(data []byte) error {
	type stored TableData
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "version", "rows", "columns", "cells", "style")
	if err != nil {
		return err
	}
	*value = TableData(decoded)
	value.Extra = extra
	return nil
}

func (value TableRow) MarshalJSON() ([]byte, error) {
	type stored TableRow
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"height"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *TableRow) UnmarshalJSON(data []byte) error {
	type stored TableRow
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "height")
	if err != nil {
		return err
	}
	*value = TableRow(decoded)
	value.Extra = extra
	return nil
}

func (value TableColumn) MarshalJSON() ([]byte, error) {
	type stored TableColumn
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"width"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *TableColumn) UnmarshalJSON(data []byte) error {
	type stored TableColumn
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "width")
	if err != nil {
		return err
	}
	*value = TableColumn(decoded)
	value.Extra = extra
	return nil
}

func (value TableCell) MarshalJSON() ([]byte, error) {
	type stored TableCell
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"text", "runs", "rowSpan", "colSpan", "hidden", "style", "borderFragments"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *TableCell) UnmarshalJSON(data []byte) error {
	type stored TableCell
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "text", "runs", "rowSpan", "colSpan", "hidden", "style", "borderFragments")
	if err != nil {
		return err
	}
	*value = TableCell(decoded)
	value.Extra = extra
	return nil
}

func (value ObjectTextRun) MarshalJSON() ([]byte, error) {
	type stored ObjectTextRun
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"text", "bold", "boldOff", "italic", "underline", "color", "colorToken", "fontId", "fontRole", "fontSize"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *ObjectTextRun) UnmarshalJSON(data []byte) error {
	type stored ObjectTextRun
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "text", "bold", "boldOff", "italic", "underline", "color", "colorToken", "fontId", "fontRole", "fontSize")
	if err != nil {
		return err
	}
	*value = ObjectTextRun(decoded)
	value.Extra = extra
	return nil
}

func (value TableStyle) MarshalJSON() ([]byte, error) {
	type stored TableStyle
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"header", "banded", "cell", "headerFill", "bandFill"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *TableStyle) UnmarshalJSON(data []byte) error {
	type stored TableStyle
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "header", "banded", "cell", "headerFill", "bandFill")
	if err != nil {
		return err
	}
	*value = TableStyle(decoded)
	value.Extra = extra
	return nil
}

func (value TableCellStyle) MarshalJSON() ([]byte, error) {
	type stored TableCellStyle
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"defaultColor", "fill", "color", "borderColor", "borderWidth", "borders", "fontId", "fontSize", "fontWidth", "lineHeight", "align", "vertical", "padding"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *TableCellStyle) UnmarshalJSON(data []byte) error {
	type stored TableCellStyle
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "defaultColor", "fill", "color", "borderColor", "borderWidth", "borders", "fontId", "fontSize", "fontWidth", "lineHeight", "align", "vertical", "padding")
	if err != nil {
		return err
	}
	*value = TableCellStyle(decoded)
	value.Extra = extra
	return nil
}

func (value TableBorderStyle) MarshalJSON() ([]byte, error) {
	type stored TableBorderStyle
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		delete(*extra, "color")
		delete(*extra, "width")
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *TableBorderStyle) UnmarshalJSON(data []byte) error {
	type stored TableBorderStyle
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "color", "width")
	if err != nil {
		return err
	}
	*value = TableBorderStyle(decoded)
	value.Extra = extra
	return nil
}

func (value ChartData) MarshalJSON() ([]byte, error) {
	type stored ChartData
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"version", "type", "title", "categories", "categoryColors", "series", "options"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *ChartData) UnmarshalJSON(data []byte) error {
	type stored ChartData
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "version", "type", "title", "categories", "categoryColors", "series", "options")
	if err != nil {
		return err
	}
	*value = ChartData(decoded)
	value.Extra = extra
	return nil
}

func (value ChartSeries) MarshalJSON() ([]byte, error) {
	type stored ChartSeries
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"name", "values", "color", "colorToken", "labelColor"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *ChartSeries) UnmarshalJSON(data []byte) error {
	type stored ChartSeries
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "name", "values", "color", "colorToken", "labelColor")
	if err != nil {
		return err
	}
	*value = ChartSeries(decoded)
	value.Extra = extra
	return nil
}

func (value ChartOptions) MarshalJSON() ([]byte, error) {
	type stored ChartOptions
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"defaultColor", "defaultFontId", "fontId", "fontRole", "fontWidth", "gridColor", "hideAxes", "legend", "dataLabels", "stacked", "valueFormat", "xTitle", "yTitle", "minimum", "maximum"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *ChartOptions) UnmarshalJSON(data []byte) error {
	type stored ChartOptions
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "defaultColor", "defaultFontId", "fontId", "fontRole", "fontWidth", "gridColor", "hideAxes", "legend", "dataLabels", "stacked", "valueFormat", "xTitle", "yTitle", "minimum", "maximum")
	if err != nil {
		return err
	}
	*value = ChartOptions(decoded)
	value.Extra = extra
	return nil
}

func (value SlideBuild) MarshalJSON() ([]byte, error) {
	type stored SlideBuild
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"version", "transition", "steps"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *SlideBuild) UnmarshalJSON(data []byte) error {
	type stored SlideBuild
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "version", "transition", "steps")
	if err != nil {
		return err
	}
	*value = SlideBuild(decoded)
	value.Extra = extra
	return nil
}

func (value SlideTransition) MarshalJSON() ([]byte, error) {
	type stored SlideTransition
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"effect", "direction", "durationMs"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *SlideTransition) UnmarshalJSON(data []byte) error {
	type stored SlideTransition
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "effect", "direction", "durationMs")
	if err != nil {
		return err
	}
	*value = SlideTransition(decoded)
	value.Extra = extra
	return nil
}

func (value BuildStep) MarshalJSON() ([]byte, error) {
	type stored BuildStep
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		for _, key := range []string{"targetIds", "effect", "direction", "durationMs", "advance", "delayMs"} {
			delete(*extra, key)
		}
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *BuildStep) UnmarshalJSON(data []byte) error {
	type stored BuildStep
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "targetIds", "effect", "direction", "durationMs", "advance", "delayMs")
	if err != nil {
		return err
	}
	*value = BuildStep(decoded)
	value.Extra = extra
	return nil
}
