// Copyright (c) 2026 the go-widgets/gpupainter authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package gpupainter

import (
	"bytes"
	"math/rand/v2"
	"testing"

	"github.com/go-gfx/gpu2d"
	"github.com/go-widgets/painter"
)

// full is every capability both PixelPainter and Painter have.
type full interface {
	painter.Painter
	painter.Clipper
	painter.Translator
	painter.MaskPainter
	painter.ImagePainter
	painter.PathPainter
	SetTextScale(n int)
}

// scene drives n random calls, the same for any painter given the seed.
func scene(p full, seed uint64, n int) {
	w, h := p.Size()
	r := rand.New(rand.NewPCG(seed, 7))
	coord := func(m int) int { return r.IntN(m+16) - 8 }
	size := func() int { return r.IntN(24) }
	col := func() painter.RGBA {
		a := uint8(255)
		if r.IntN(2) == 0 {
			a = uint8(r.IntN(256))
		}
		return painter.RGBA{R: uint8(r.IntN(256)), G: uint8(r.IntN(256)), B: uint8(r.IntN(256)), A: a}
	}
	rect := func() painter.Rect { return painter.Rect{X: coord(w), Y: coord(h), W: size(), H: size()} }
	bytesOf := func(n int) []byte {
		b := make([]byte, max(n, 0))
		for i := range b {
			if r.IntN(3) > 0 {
				b[i] = uint8(r.IntN(256))
			}
		}
		return b
	}
	pt := func() (float64, float64) { return float64(coord(w)) + r.Float64(), float64(coord(h)) + r.Float64() }
	path := func() *painter.Path {
		x, y := pt()
		q := painter.NewPath().MoveTo(x, y)
		for range 2 + r.IntN(4) {
			x, y = pt()
			switch r.IntN(4) {
			case 0:
				cx, cy := pt()
				q.QuadTo(cx, cy, x, y)
			case 1:
				c1x, c1y := pt()
				c2x, c2y := pt()
				q.CubicTo(c1x, c1y, c2x, c2y, x, y)
			default:
				q.LineTo(x, y)
			}
		}
		if r.IntN(2) == 0 {
			q.Close()
		}
		return q
	}
	const text = "Hello, GPU 0123 #!?"
	for range n {
		switch r.IntN(16) {
		case 0, 1:
			p.FillRect(rect(), col())
		case 2:
			p.FillRoundRect(rect(), r.IntN(14)-2, col())
		case 3:
			p.StrokeRect(rect(), col(), r.IntN(4))
		case 4:
			p.StrokeRoundRect(rect(), r.IntN(14)-2, col(), r.IntN(4))
		case 5:
			p.PutPixel(coord(w), coord(h), col())
		case 6:
			p.SetTextScale(1 + r.IntN(3))
			p.Text(coord(w), coord(h), text[:r.IntN(len(text))], col())
		case 7:
			d := rect()
			stride := d.W + r.IntN(3)
			p.DrawMask(d, bytesOf(stride*d.H), stride, col())
		case 8:
			sw, sh := 1+r.IntN(10), 1+r.IntN(10)
			p.DrawImage(rect(), bytesOf(4*sw*sh), sw, sh)
		case 9:
			p.FillPath(path(), col(), painter.FillRule(r.IntN(2)))
		case 10:
			p.StrokePath(path(), col(), 0.5+3*r.Float64())
		case 11:
			c := rect()
			c.W += 16
			c.H += 16
			p.PushClip(c)
		case 12:
			p.PushTranslate(r.IntN(13)-6, r.IntN(13)-6)
		case 13:
			p.PopClip()
		case 14:
			p.PopTranslate()
		default:
			p.FillRect(rect(), col())
		}
	}
}

// TestMatchesPixelPainter renders random scenes with PixelPainter and with
// Painter, rasterized by gpu2d's CPU reference, and compares every byte.
func TestMatchesPixelPainter(t *testing.T) {
	const w, h, seeds = 64, 48, 2000
	kinds := map[gpu2d.Kind]int{}
	for seed := range uint64(seeds) {
		want := make([]byte, 4*w*h)
		for i := range want {
			want[i] = uint8(i * 5)
		}
		got := append([]byte(nil), want...)
		scene(painter.NewPixelPainter(want, w, h), seed, 40)
		l, err := gpu2d.NewList(w, h)
		if err != nil {
			t.Fatal(err)
		}
		scene(New(l), seed, 40)
		if err := l.Err(); err != nil {
			t.Fatal(err)
		}
		for _, in := range l.Instances() {
			kinds[in.Kind]++
		}
		if err := l.Rasterize(got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, got) {
			for i := range want {
				if want[i] != got[i] {
					p := i / 4
					t.Fatalf("seed %d: first difference at (%d,%d) channel %d: PixelPainter %d, gpupainter %d",
						seed, p%w, p/w, i%4, want[i], got[i])
				}
			}
		}
	}
	t.Logf("%d scenes: %d fill, %d outline, %d mask (incl. text), %d alpha (paths), %d image", seeds,
		kinds[gpu2d.KindFill], kinds[gpu2d.KindOutline], kinds[gpu2d.KindMask], kinds[gpu2d.KindAlpha], kinds[gpu2d.KindImage])
	for k := gpu2d.KindFill; k <= gpu2d.KindImage; k++ {
		if kinds[k] < seeds/2 {
			t.Fatalf("kind %d recorded only %d times", k, kinds[k])
		}
	}
}

func TestAccessorsAndNoOps(t *testing.T) {
	l, _ := gpu2d.NewList(8, 8)
	p := New(l)
	if p.List() != l {
		t.Fatal("List is not the list given to New")
	}
	p.SetTextScale(0)
	if p.TextScale() != 1 {
		t.Fatalf("TextScale after SetTextScale(0) = %d", p.TextScale())
	}
	p.Text(0, 0, "", painter.RGBA{A: 255})
	p.Text(0, 0, "x", painter.RGBA{})
	p.FillPath(nil, painter.RGBA{A: 255}, painter.NonZero)
	p.StrokePath(nil, painter.RGBA{A: 255}, 1)
	if n := len(l.Instances()); n != 0 {
		t.Fatalf("%d instances for no-ops", n)
	}
}

// The comparison can fail.
func TestComparisonCanFail(t *testing.T) {
	a := make([]byte, 4*16*16)
	painter.NewPixelPainter(a, 16, 16).Text(1, 1, "A", painter.RGBA{A: 255})
	l, _ := gpu2d.NewList(16, 16)
	New(l).Text(1, 1, "B", painter.RGBA{A: 255})
	b := make([]byte, len(a))
	_ = l.Rasterize(b)
	if bytes.Equal(a, b) {
		t.Fatal("A and B compared equal")
	}
}
