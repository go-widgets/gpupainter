// Copyright (c) 2026 the go-widgets/gpupainter authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package gpupainter

import (
	"github.com/go-gfx/gpu2d"
	"github.com/go-widgets/painter"
)

// maxPatch bounds the patch a run of pixels becomes: 2^20 pixels (4 MiB). A
// run whose bounding box would pass it is flushed first.
const maxPatch = 1 << 20

// pix is one pending PutPixel, in the caller's coordinates.
type pix struct {
	x, y int
	c    gpu2d.RGBA
}

// A widget that paints pixel by pixel (a colour picker's square, a hue strip)
// would otherwise make one instance per pixel: the go-widgets gallery is
// 25,181 single pixels out of 28,753 instances, and a GPU walks every one of
// them for every pixel of its tile. PixelPainter blends each PutPixel onto
// the ground on its own, so a run of them at distinct positions under one
// clip and translation is the same as one image whose untouched texels are
// transparent: an image texel blends exactly as PutPixel does (opaque copies,
// 0 leaves, else blends). Runs are flushed before any other call, before a
// pixel lands twice, and when their box would pass maxPatch.

// PutPixel paints one pixel.
func (p *Painter) PutPixel(x, y int, c painter.RGBA) {
	if c.A == 0 {
		return
	}
	k := [2]int{x, y}
	if _, dup := p.seen[k]; dup {
		p.flushPixels()
	}
	if len(p.pending) > 0 {
		x0, y0 := min(p.x0, x), min(p.y0, y)
		x1, y1 := max(p.x1, x+1), max(p.y1, y+1)
		if int64(x1-x0)*int64(y1-y0) > maxPatch {
			p.flushPixels()
		}
	}
	if len(p.pending) == 0 {
		p.x0, p.y0, p.x1, p.y1 = x, y, x+1, y+1
	} else {
		p.x0, p.y0 = min(p.x0, x), min(p.y0, y)
		p.x1, p.y1 = max(p.x1, x+1), max(p.y1, y+1)
	}
	if p.seen == nil {
		p.seen = map[[2]int]struct{}{}
	}
	p.seen[k] = struct{}{}
	p.pending = append(p.pending, pix{x, y, rgba(c)})
}

// flushPixels records the pending run: a single pixel as a 1 × 1 fill, more
// as one image over their bounding box.
func (p *Painter) flushPixels() {
	switch len(p.pending) {
	case 0:
		return
	case 1:
		q := p.pending[0]
		p.l.FillRect(gpu2d.Rect{X: q.x, Y: q.y, W: 1, H: 1}, q.c)
	default:
		w, h := p.x1-p.x0, p.y1-p.y0
		n := 4 * w * h
		if cap(p.patch) < n {
			p.patch = make([]byte, n)
		}
		patch := p.patch[:n]
		clear(patch)
		for _, q := range p.pending {
			o := 4 * ((q.y-p.y0)*w + q.x - p.x0)
			patch[o], patch[o+1], patch[o+2], patch[o+3] = q.c.R, q.c.G, q.c.B, q.c.A
		}
		p.l.DrawImage(gpu2d.Rect{X: p.x0, Y: p.y0, W: w, H: h}, patch, w, h)
	}
	p.pending = p.pending[:0]
	clear(p.seen)
}

// Flush records any pixels still pending. List flushes too; a caller that
// keeps the list from New and hands it to a renderer calls Flush first.
func (p *Painter) Flush() { p.flushPixels() }
