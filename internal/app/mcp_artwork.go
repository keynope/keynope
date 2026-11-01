//go:build !js && !keynope_sitegen

package app

// Native counterparts of Josh Gonggryp's keynope-slides artwork helpers.
// The particle equations, palette, layer masks and block metrics deliberately
// match the contributed recipes. All file operations use the MCP workspace.
import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"math"
	"math/rand/v2"
	"path/filepath"
	"strconv"
	"strings"
)

type automationRectangle struct {
	RowStart    int `json:"row_start"`
	RowEnd      int `json:"row_end"`
	ColumnStart int `json:"column_start"`
	ColumnEnd   int `json:"column_end"`
}
type automationLayer struct {
	Name       string                `json:"name"`
	Rectangles []automationRectangle `json:"rectangles"`
}

func automationDiagram(input automationArguments) (map[string]any, []byte, error) {
	if len(input.Text) > 256<<10 || len(input.Layers) > 64 {
		return nil, nil, errors.New("diagram limit: 256 KiB text, 64 layers")
	}
	lines := strings.Split(strings.TrimRight(input.Text, "\n"), "\n")
	if len(lines) > 512 {
		return nil, nil, errors.New("diagram exceeds 512 rows")
	}
	layers := input.Layers
	if len(layers) == 0 {
		layers = []automationLayer{{Name: "all"}}
	}
	out := []map[string]any{}
	for _, layer := range layers {
		if len(layer.Rectangles) > 256 {
			return nil, nil, errors.New("layer exceeds 256 rectangles")
		}
		for _, r := range layer.Rectangles {
			if r.RowStart < 0 || r.ColumnStart < 0 || r.RowEnd < r.RowStart || r.ColumnEnd < r.ColumnStart {
				return nil, nil, errors.New("rectangles need ordered, non-negative inclusive bounds")
			}
		}
		result := []string{}
		for row, line := range lines {
			runes := []rune(line)
			if len(runes) > 512 {
				return nil, nil, errors.New("diagram exceeds 512 columns")
			}
			for column, ch := range runes {
				inside := len(layer.Rectangles) == 0
				for _, r := range layer.Rectangles {
					inside = inside || row >= r.RowStart && row <= r.RowEnd && column >= r.ColumnStart && column <= r.ColumnEnd
				}
				if !inside || ch == ' ' {
					runes[column] = '\u00a0'
				}
			}
			text := strings.TrimRight(string(runes), "\u00a0")
			if text == "" {
				text = "\u00a0"
			}
			result = append(result, text)
		}
		for len(result) > 1 && result[len(result)-1] == "\u00a0" {
			result = result[:len(result)-1]
		}
		text := strings.Join(result, "\n")
		if len(text) > 100000 {
			return nil, nil, errors.New("layer exceeds native text limit; reduce the diagram dimensions")
		}
		out = append(out, map[string]any{"name": layer.Name, "text": text, "query": "render=truetype&modern-font=mono&modern-size=32&modern-width=100&modern-line-height=1.15"})
	}
	return map[string]any{"layers": out}, nil, nil
}

func (a *deckAutomation) pixelart(ctx context.Context, input automationArguments) (map[string]any, []byte, error) {
	if input.Columns < 1 || input.Columns > 160 {
		return nil, nil, errors.New("columns must be 1–160")
	}
	mode := input.Mode
	if mode == "" {
		mode = "alpha"
	}
	if mode != "alpha" && mode != "dark" && mode != "nonwhite" {
		return nil, nil, errors.New("mode must be alpha, dark or nonwhite")
	}
	threshold := .4
	if input.Threshold != nil {
		threshold = *input.Threshold
	}
	if !finite(threshold) || threshold < 0 || threshold > 1 {
		return nil, nil, errors.New("threshold must be 0–1")
	}
	if input.Colour != "" {
		if _, err := artworkColour(input.Colour); err != nil {
			return nil, nil, err
		}
	}
	data, err := a.read(input.Path, 50<<20)
	if err != nil {
		return nil, nil, err
	}
	if looksLikeSVG(data) {
		canonical, w, h, err := sanitizeSVG(ctx, data)
		if err != nil {
			return nil, nil, err
		}
		w, h = boundedImageSize(w, h, 2048)
		data, err = rasterizeSVG(ctx, canonical, w, h)
		if err != nil {
			return nil, nil, err
		}
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return nil, nil, errors.New("image exceeds 32 megapixels")
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	ink := func(c color.Color) bool {
		p := color.NRGBAModel.Convert(c).(color.NRGBA)
		if p.A <= 128 {
			return false
		}
		sum := int(p.R) + int(p.G) + int(p.B)
		return mode == "alpha" || mode == "dark" && sum < 300 || mode == "nonwhite" && 765-sum > 90
	}
	b := im.Bounds()
	crop := image.Rectangle{Min: b.Max, Max: b.Min}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		for x := b.Min.X; x < b.Max.X; x++ {
			if ink(im.At(x, y)) {
				crop.Min.X = min(crop.Min.X, x)
				crop.Min.Y = min(crop.Min.Y, y)
				crop.Max.X = max(crop.Max.X, x+1)
				crop.Max.Y = max(crop.Max.Y, y+1)
			}
		}
	}
	if crop.Empty() {
		return nil, nil, errors.New("image contains no ink for the selected mode")
	}
	rows := max(1, int(math.RoundToEven(float64(crop.Dy())/float64(crop.Dx())*float64(input.Columns)*.82)))
	if rows > 512 || rows*input.Columns > 32768 {
		return nil, nil, errors.New("pixel grid too tall; use fewer columns")
	}
	w, h := input.Columns*8, rows*8
	pixels := resizeLanczosRGBA(im, crop, w, h)
	grid := make([][]string, rows)
	runs := []sceneRun{}
	appendRun := func(text, colour string) {
		if len(runs) > 0 && runs[len(runs)-1].Color == colour {
			runs[len(runs)-1].Text += text
		} else {
			runs = append(runs, sceneRun{Text: text, Color: colour})
		}
	}
	for r := 0; r < rows; r++ {
		grid[r] = make([]string, input.Columns)
		for c := 0; c < input.Columns; c++ {
			count, red, green, blue := 0, 0, 0, 0
			for y := r * 8; y < r*8+8; y++ {
				for x := c * 8; x < c*8+8; x++ {
					p := pixels[y*w+x]
					if ink(color.NRGBA{R: p.r, G: p.g, B: p.b, A: p.a}) {
						count++
						red += int(p.r)
						green += int(p.g)
						blue += int(p.b)
					}
				}
			}
			grid[r][c] = "."
			if count > 0 && float64(count)/64 >= threshold {
				colour := input.Colour
				if colour == "" {
					colour = fmt.Sprintf("#%02x%02x%02x", red/count, green/count, blue/count)
				}
				grid[r][c] = colour
			}
		}
		end := input.Columns
		for end > 0 && grid[r][end-1] == "." {
			end--
		}
		for _, c := range grid[r][:end] {
			if c == "." {
				appendRun("\u00a0", "")
			} else {
				appendRun("▆", c)
			}
		}
		if r < rows-1 {
			appendRun("\n", "")
		}
	}
	var plain strings.Builder
	for _, run := range runs {
		plain.WriteString(run.Text)
	}
	if _, err := encodeModernRunsMetadata(plain.String(), "", runs, true); err != nil {
		return nil, nil, fmt.Errorf("pixel artwork exceeds native rich-text capacity; reduce columns: %w", err)
	}
	e := Element{Kind: "text", Text: plain.String(), Query: "render=truetype&modern-font=mono&modern-size=20&modern-width=105&modern-line-height=0.76"}
	return map[string]any{"columns": input.Columns, "rows": rows, "grid": grid, "element": e, "text_runs": runs, "warnings": []string{"The contributor's partial-block glyph ▆ is not in bundled Go Mono; this editable-text recipe uses a system glyph fallback. For font-independent portable artwork, import the image with image-style=retro and glyph=blocks instead."}, "note": "Add element, then set-scene-text with text_runs using the returned object ID. Resize through font size."}, nil, nil
}

type automationFireworks struct {
	Width      int        `json:"width,omitempty"`
	Height     int        `json:"height,omitempty"`
	Seconds    float64    `json:"seconds,omitempty"`
	DelayMS    int        `json:"delay_ms,omitempty"`
	Rockets    int        `json:"rockets,omitempty"`
	SalvoEvery *float64   `json:"salvo_every,omitempty"`
	BurstMin   float64    `json:"burst_min,omitempty"`
	BurstMax   float64    `json:"burst_max,omitempty"`
	Colours    []string   `json:"colours,omitempty"`
	Seed       uint64     `json:"seed,omitempty"`
	Avoid      *sceneRect `json:"avoid,omitempty"` // authored grid rectangle
}

func artworkColour(s string) (color.NRGBA, error) {
	if len(s) != 7 || s[0] != '#' {
		return color.NRGBA{}, errors.New("colour must be #RRGGBB")
	}
	v, err := strconv.ParseUint(s[1:], 16, 24)
	if err != nil {
		return color.NRGBA{}, errors.New("colour must be #RRGGBB")
	}
	return color.NRGBA{R: byte(v >> 16), G: byte(v >> 8), B: byte(v), A: 255}, nil
}

func (a *deckAutomation) createAnimation(ctx context.Context, input automationArguments) (map[string]any, []byte, error) {
	if !strings.EqualFold(filepath.Ext(input.Path), ".gif") {
		return nil, nil, errors.New("animation destination must end in .gif")
	}
	if _, err := a.relative(input.Path); err != nil {
		return nil, nil, err
	}
	c := automationFireworks{}
	if input.Animation != nil {
		c = *input.Animation
	}
	if c.Width == 0 {
		c.Width = 640
	}
	if c.Height == 0 {
		c.Height = 354
	}
	if c.Seconds == 0 {
		c.Seconds = 10
	}
	if c.DelayMS == 0 {
		c.DelayMS = 80
	}
	if c.Rockets == 0 {
		c.Rockets = 36
	}
	if c.BurstMin == 0 {
		c.BurstMin = 170
	}
	if c.BurstMax == 0 {
		c.BurstMax = 330
	}
	salvo := 2.5
	if c.SalvoEvery != nil {
		salvo = *c.SalvoEvery
	}
	if c.Width < 16 || c.Height < 16 || c.Width > 2048 || c.Height > 2048 || !finite(c.Seconds) || c.Seconds < .1 || c.Seconds > 60 || c.DelayMS < 20 || c.DelayMS > 1000 || c.DelayMS%10 != 0 || c.Rockets < 1 || c.Rockets > 100 || !finite(salvo) || salvo < 0 || salvo > 60 || salvo > 0 && salvo < .25 || !finite(c.BurstMin) || !finite(c.BurstMax) || c.BurstMin < 1 || c.BurstMax < c.BurstMin || c.BurstMax > 1920 {
		return nil, nil, errors.New("invalid animation dimensions/timing/particles; use 16–2048 pixels, .1–60 seconds, 20–1000ms delay (10ms steps), 1–100 rockets and ordered burst radii <=1920")
	}
	frames := int(math.RoundToEven(c.Seconds * 1000 / float64(c.DelayMS)))
	if frames < 2 || int64(c.Width)*int64(c.Height)*int64(frames) > 32_000_000 {
		return nil, nil, errors.New("animation exceeds 32 megapixels or has fewer than two frames")
	}
	if len(c.Colours) == 0 {
		c.Colours = []string{"#ff5555", "#ffff55", "#55ffff", "#ff55ff", "#55ff55", "#ffaa33", "#7fdfff", "#ffffff"}
	}
	if len(c.Colours) > 64 {
		return nil, nil, errors.New("at most 64 colours")
	}
	palette := color.Palette{color.NRGBA{}, color.White, color.NRGBA{255, 255, 160, 255}, color.NRGBA{255, 170, 51, 255}}
	for _, value := range c.Colours {
		p, err := artworkColour(value)
		if err != nil {
			return nil, nil, err
		}
		for _, k := range []float64{1, .7, .45} {
			palette = append(palette, color.NRGBA{byte(float64(p.R) * k), byte(float64(p.G) * k), byte(float64(p.B) * k), 255})
		}
	}
	scale := float64(c.Width) / 1920
	grid := max(2, int(math.Round(8*scale)))
	var avoid *sceneRect
	if c.Avoid != nil {
		r := *c.Avoid
		if !finite(r.X) || !finite(r.Y) || !finite(r.Width) || !finite(r.Height) || r.X < 0 || r.Y < 0 || r.Width <= 0 || r.Height <= 0 || r.X+r.Width > 245 || r.Y+r.Height > 56 {
			return nil, nil, errors.New("avoid must be a valid grid rectangle")
		}
		r = sceneRect{r.X / 245 * float64(c.Width), r.Y / 56 * 1080 * scale, r.Width / 245 * float64(c.Width), r.Height / 56 * 1080 * scale}
		avoid = &r
	}
	rng := rand.New(rand.NewPCG(c.Seed, c.Seed^0x6b65796e6f7065))
	uniform := func(a, b float64) float64 { return a + rng.Float64()*(b-a) }
	type particle struct {
		angle, speed, twinkle float64
		colour                int
	}
	type rocket struct {
		start, bx, by, x0, radius float64
		rise, life                int
		willow                    bool
		particles                 []particle
	}
	starts := []float64{}
	if salvo > 0 {
		for s := .5; s < c.Seconds; s += salvo {
			st := s * float64(frames) / c.Seconds
			starts = append(starts, st, st+2, st+4)
		}
	}
	for len(starts) < c.Rockets {
		starts = append(starts, uniform(0, float64(frames)))
	}
	if len(starts) > c.Rockets {
		starts = starts[:c.Rockets]
	}
	rockets := []rocket{}
	for _, start := range starts {
		r := rocket{start: start, rise: 8 + rng.IntN(4), life: 15 + rng.IntN(7)}
		for attempt := 0; attempt < 20; attempt++ {
			r.bx = uniform(90, 1830) * scale
			r.by = uniform(70, 720) * scale
			if avoid == nil || r.bx < avoid.X || r.bx > avoid.X+avoid.Width || r.by < avoid.Y || r.by > avoid.Y+avoid.Height {
				break
			}
		}
		r.x0 = r.bx + uniform(-60, 60)*scale
		kind := rng.IntN(21)
		r.willow = kind >= 18
		r.radius = uniform(c.BurstMin, c.BurstMax) * scale
		if r.willow {
			r.radius *= 1.15
			r.life = 24 + rng.IntN(7)
		}
		c1, c2 := rng.IntN(len(c.Colours)), rng.IntN(len(c.Colours))
		n := 48 + rng.IntN(37)
		for p := 0; p < n; p++ {
			speed := 1.
			if kind < 8 || kind >= 12 {
				speed = uniform(.9, 1)
				if p%3 == 0 {
					speed = uniform(.35, .9)
				}
			}
			colour := c1
			if kind >= 12 && kind < 18 && p%2 == 0 {
				colour = c2
			}
			r.particles = append(r.particles, particle{2*math.Pi*float64(p)/float64(n) + uniform(-.05, .05), speed, rng.Float64(), colour})
		}
		rockets = append(rockets, r)
	}
	out := gif.GIF{LoopCount: 0}
	for frame := 0; frame < frames; frame++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		im := image.NewPaletted(image.Rect(0, 0, c.Width, c.Height), palette)
		block := func(x, y float64, size, index int) {
			gx, gy := int(math.Floor(x/float64(grid)))*grid, int(math.Floor(y/float64(grid)))*grid
			for yy := max(0, gy); yy < min(c.Height, gy+size); yy++ {
				for xx := max(0, gx); xx < min(c.Width, gx+size); xx++ {
					im.SetColorIndex(xx, yy, uint8(index))
				}
			}
		}
		for _, r := range rockets {
			age := math.Mod(float64(frame)-r.start+2*float64(frames), float64(frames))
			if age < float64(r.rise) {
				for back := 3; back >= 0; back-- {
					e := 1 - math.Pow(1-math.Max(0, age-float64(back)*.6)/float64(r.rise), 2)
					sz, index := grid/2, 3
					if back == 0 {
						sz, index = grid, 2
					}
					block(r.x0+(r.bx-r.x0)*e, float64(c.Height)+10*scale+(r.by-float64(c.Height)-10*scale)*e, sz, index)
				}
				continue
			}
			age -= float64(r.rise)
			if age > float64(r.life) {
				continue
			}
			fraction := age / float64(r.life)
			if age < 1 {
				block(r.bx-float64(grid), r.by-float64(grid), grid*3, 1)
			}
			for _, p := range r.particles {
				if fraction > .78 && math.Mod(p.twinkle+float64(frame)*.37, 1) < .45 {
					continue
				}
				point := func(a float64) (float64, float64) {
					const drag = .86
					k := (1 - math.Pow(drag, a)) / (1 - drag)
					v := r.radius * p.speed * (1 - drag) / (1 - math.Pow(drag, 16))
					g := 1.3 * scale
					if r.willow {
						g = 2.2 * scale
					}
					return r.bx + math.Cos(p.angle)*v*k, r.by + math.Sin(p.angle)*v*k + g*(a-k)/(1-drag)
				}
				if fraction < .8 {
					for back := 2; back >= 1; back-- {
						if age-float64(back) > 0 {
							x, y := point(age - float64(back))
							sz := grid
							if back == 2 {
								sz = grid / 2
							}
							block(x, y, sz, 4+p.colour*3+back)
						}
					}
				}
				shade := 0
				if fraction >= .55 {
					shade = 1
				}
				if fraction >= .8 {
					shade = 2
				}
				index := 4 + p.colour*3 + shade
				if fraction < .1 {
					index = 1
				}
				x, y := point(age)
				block(x, y, grid, index)
			}
		}
		out.Image = append(out.Image, im)
		out.Delay = append(out.Delay, c.DelayMS/10)
		out.Disposal = append(out.Disposal, gif.DisposalBackground)
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &out); err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	saved, err := a.write(input.Path, encoded.Bytes(), input.Overwrite)
	if err != nil {
		return nil, nil, err
	}
	result := a.mutationResult(input)
	result["output"] = saved
	result["bytes"] = encoded.Len()
	result["frames"] = frames
	result["width"] = c.Width
	result["height"] = c.Height
	result["rockets"] = len(rockets)
	result["geometry"] = sceneRect{0, 0, 245, float64(c.Height) / float64(c.Width) * 1920 / 1080 * 56}
	return result, nil, nil
}
