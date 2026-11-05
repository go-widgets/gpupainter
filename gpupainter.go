// Copyright (c) 2026 the go-widgets/gpupainter authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

// Package gpupainter is a go-widgets painter.Painter that records what a
// widget draws into a go-gfx/gpu2d list, which a GPU back-end (gpu2d/webgpu
// in a browser, gpu2d/metal on a Mac) then draws in one submission. It
// implements every capability PixelPainter has (clip, translation, masks,
// images, paths, the built-in font and its scale) and the list it records
// renders byte-identical to what PixelPainter draws.
package gpupainter

import (
	"github.com/go-gfx/gfx/vector"
	"github.com/go-gfx/gpu2d"
	"github.com/go-widgets/painter"
)

// Painter records into a gpu2d list. The list is the caller's: Reset it
// between frames, and take it with List (or call Flush) before handing it to
// a renderer, so that pixels still being batched are recorded.
type Painter struct {
	l         *gpu2d.List
	rz        vector.Rasterizer
	off       [][2]int
	textScale int
	glyphs    []byte // scratch RGBA the built-in font is drawn into

	// The run of PutPixel calls being batched (see pixels.go).
	pending        []pix
	seen           map[[2]int]struct{}
	x0, y0, x1, y1 int
	patch          []byte
}

// The capabilities widgets type-assert for, all present.
var (
	_ painter.Painter      = (*Painter)(nil)
	_ painter.Clipper      = (*Painter)(nil)
	_ painter.Translator   = (*Painter)(nil)
	_ painter.MaskPainter  = (*Painter)(nil)
	_ painter.ImagePainter = (*Painter)(nil)
	_ painter.PathPainter  = (*Painter)(nil)
)

// New returns a Painter recording into l.
func New(l *gpu2d.List) *Painter { return &Painter{l: l, textScale: 1} }

// List flushes pending pixels and returns the list the painter records into.
func (p *Painter) List() *gpu2d.List {
	p.flushPixels()
	return p.l
}

func rect(r painter.Rect) gpu2d.Rect { return gpu2d.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H} }
func rgba(c painter.RGBA) gpu2d.RGBA { return gpu2d.RGBA{R: c.R, G: c.G, B: c.B, A: c.A} }

// Size is the list's surface size.
func (p *Painter) Size() (int, int) { return p.l.Size() }

// FillRect fills r.
func (p *Painter) FillRect(r painter.Rect, c painter.RGBA) {
	p.flushPixels()
	p.l.FillRect(rect(r), rgba(c))
}

// StrokeRect paints lineW concentric outlines inward from r's edge.
func (p *Painter) StrokeRect(r painter.Rect, c painter.RGBA, lineW int) {
	p.flushPixels()
	p.l.StrokeRect(rect(r), rgba(c), lineW)
}

// FillRoundRect fills r with anti-aliased corners of the given radius.
func (p *Painter) FillRoundRect(r painter.Rect, radius int, c painter.RGBA) {
	p.flushPixels()
	p.l.FillRoundRect(rect(r), radius, rgba(c))
}

// StrokeRoundRect paints lineW concentric rounded outlines.
func (p *Painter) StrokeRoundRect(r painter.Rect, radius int, c painter.RGBA, lineW int) {
	p.flushPixels()
	p.l.StrokeRoundRect(rect(r), radius, rgba(c), lineW)
}

// PushClip confines drawing to r, translated and intersected as PixelPainter
// does.
func (p *Painter) PushClip(r painter.Rect) {
	p.flushPixels()
	p.l.PushClip(rect(r))
}

// PopClip removes the innermost clip.
func (p *Painter) PopClip() {
	p.flushPixels()
	p.l.PopClip()
}

// PushTranslate shifts what is drawn after it. The painter keeps its own copy
// of the offset for paths, which reach the rasterizer in surface pixels.
func (p *Painter) PushTranslate(dx, dy int) {
	p.flushPixels()
	ox, oy := p.offset()
	p.off = append(p.off, [2]int{ox + dx, oy + dy})
	p.l.PushTranslate(dx, dy)
}

// PopTranslate removes the innermost translation.
func (p *Painter) PopTranslate() {
	p.flushPixels()
	if n := len(p.off); n > 0 {
		p.off = p.off[:n-1]
	}
	p.l.PopTranslate()
}

func (p *Painter) offset() (int, int) {
	if n := len(p.off); n > 0 {
		return p.off[n-1][0], p.off[n-1][1]
	}
	return 0, 0
}

// DrawMask paints ink through 8-bit coverage.
func (p *Painter) DrawMask(dst painter.Rect, mask []byte, stride int, ink painter.RGBA) {
	p.flushPixels()
	p.l.DrawMask(rect(dst), mask, stride, rgba(ink))
}

// DrawImage draws RGBA pixels scaled into dst by nearest neighbour.
func (p *Painter) DrawImage(dst painter.Rect, src []byte, srcW, srcH int) {
	p.flushPixels()
	p.l.DrawImage(rect(dst), src, srcW, srcH)
}

// FillPath fills pth with c under rule.
func (p *Painter) FillPath(pth *painter.Path, c painter.RGBA, rule painter.FillRule) {
	p.flushPixels()
	if pth == nil || c.A == 0 {
		return
	}
	w, h := p.l.Size()
	if cov, ox, oy, bw, bh, ok := p.rz.Fill(p.shift(pth), rule, w, h); ok {
		p.l.DrawAlpha(cov, ox, oy, bw, bh, rgba(c))
	}
}

// StrokePath paints pth's outline width pixels wide.
func (p *Painter) StrokePath(pth *painter.Path, c painter.RGBA, width float64) {
	p.flushPixels()
	if pth == nil || c.A == 0 {
		return
	}
	w, h := p.l.Size()
	if cov, ox, oy, bw, bh, ok := p.rz.Stroke(p.shift(pth), width, w, h); ok {
		p.l.DrawAlpha(cov, ox, oy, bw, bh, rgba(c))
	}
}

// shift moves a path by the translation in force, as PixelPainter does
// since go-widgets/painter v0.16.2.
func (p *Painter) shift(pth *painter.Path) *painter.Path {
	ox, oy := p.offset()
	if ox == 0 && oy == 0 {
		return pth
	}
	dx, dy := float64(ox), float64(oy)
	q := painter.NewPath()
	pth.Each(func(op vector.PathOp, pts ...[2]float64) {
		switch op {
		case vector.OpMove:
			q.MoveTo(pts[0][0]+dx, pts[0][1]+dy)
		case vector.OpLine:
			q.LineTo(pts[0][0]+dx, pts[0][1]+dy)
		case vector.OpQuad:
			q.QuadTo(pts[0][0]+dx, pts[0][1]+dy, pts[1][0]+dx, pts[1][1]+dy)
		case vector.OpCubic:
			q.CubicTo(pts[0][0]+dx, pts[0][1]+dy, pts[1][0]+dx, pts[1][1]+dy, pts[2][0]+dx, pts[2][1]+dy)
		default:
			q.Close()
		}
	})
	return q
}

// SetTextScale magnifies the built-in font by a whole number (1 at least).
func (p *Painter) SetTextScale(n int) { p.textScale = max(n, 1) }

// TextScale is the magnification of the built-in font.
func (p *Painter) TextScale() int { return p.textScale }

// TextWidth is how wide s is drawn in the built-in font.
func (p *Painter) TextWidth(s string) int {
	return painter.NewPixelPainter(nil, 0, 0).TextWidth(s) * p.textScale
}

// TextHeight is how tall a line of the built-in font is drawn.
func (p *Painter) TextHeight() int {
	return painter.NewPixelPainter(nil, 0, 0).TextHeight() * p.textScale
}

// Text paints s in the built-in 5 × 7 font. PixelPainter lights each glyph
// bit with PutPixel (or an n × n fill when magnified) in the ink, and glyphs
// never overlap; so the string is drawn once in opaque white into a scratch
// buffer with PixelPainter itself, and its alpha goes through DrawMask with
// the real ink. Coverage 255 blends exactly as PutPixel(ink) does.
func (p *Painter) Text(x, y int, s string, ink painter.RGBA) {
	p.flushPixels()
	if ink.A == 0 || s == "" {
		return
	}
	w, h := p.TextWidth(s), p.TextHeight() // both positive: s is not empty
	n := 4 * w * h
	if cap(p.glyphs) < n {
		p.glyphs = make([]byte, n)
	}
	buf := p.glyphs[:n]
	clear(buf)
	pp := painter.NewPixelPainter(buf, w, h)
	pp.SetTextScale(p.textScale)
	pp.Text(0, 0, s, painter.RGBA{R: 255, G: 255, B: 255, A: 255})
	// The alpha channel, in place: byte i of the mask is pixel i's alpha.
	for i := range w * h {
		buf[i] = buf[4*i+3]
	}
	p.l.DrawMask(gpu2d.Rect{X: x, Y: y, W: w, H: h}, buf[:w*h], w, rgba(ink))
}
