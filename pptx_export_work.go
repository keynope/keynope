package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
)

var errPPTXExportBudget = errors.New("PowerPoint export exceeds aggregate resource budget")

// Request-owned, never stored in the deck or shared between simultaneous exports.
// Per-image decoder limits still apply. This bounds repeated captures across
// objects/slides; the package writer independently bounds compressed output.
type pptxExportWork struct {
	ctx          context.Context
	pixelsLeft   int64
	packageBytes int64
	err          error
	slide        int
	captures     map[string]pptxRenderedCapture
}

func newPPTXExportWork(ctx context.Context) *pptxExportWork {
	return &pptxExportWork{ctx: ctx, pixelsLeft: 128_000_000, packageBytes: 128 << 20}
}

func (w *pptxExportWork) check() error {
	if w == nil {
		return nil
	}
	if w.err == nil {
		w.err = w.ctx.Err()
	}
	return w.err
}

func (w *pptxExportWork) pixels(width, height float64, frames int) error {
	if err := w.check(); err != nil || w == nil {
		return err
	}
	if frames < 1 {
		frames = 1
	}
	cost := math.Ceil(width) * math.Ceil(height) * float64(frames)
	if width <= 0 || height <= 0 || math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 || cost > float64(w.pixelsLeft) {
		w.err = fmt.Errorf("%w: image conversion exceeds 128 million pixel-passes; reduce image dimensions/frames or export fewer slides", errPPTXExportBudget)
		return w.err
	}
	w.pixelsLeft -= int64(cost)
	return nil
}

func firstPPTXWork(values []*pptxExportWork) *pptxExportWork {
	if len(values) > 0 {
		return values[0]
	}
	return nil
}

type pptxExportWriter struct {
	target io.Writer
	work   *pptxExportWork
	left   int64
}

func (w *pptxExportWriter) Write(data []byte) (int, error) {
	if err := w.work.check(); err != nil {
		return 0, err
	}
	if int64(len(data)) > w.left {
		return 0, fmt.Errorf("%w: encoded output exceeds its limit", errPPTXExportBudget)
	}
	n, err := w.target.Write(data)
	w.left -= int64(n)
	return n, err
}
