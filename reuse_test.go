// Copyright (c) 2026 the go-widgets/gpupainter authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package gpupainter

import (
	"bytes"
	"slices"
	"testing"

	"github.com/go-gfx/gpu2d"
	"github.com/go-widgets/painter"
)

// frame draws one steady frame of the shapes a gallery frame is made of:
// fills, rounded boxes, clipped and translated text at two scales, a run of
// pixels, a mask and an image. It draws no path (a translated path builds a
// shifted copy, which is the caller's data, not the painter's scratch).
func frame(p *Painter) {
	ink := painter.RGBA{R: 20, G: 30, B: 40, A: 255}
	p.FillRect(painter.Rect{X: 0, Y: 0, W: 200, H: 120}, painter.RGBA{R: 240, G: 240, B: 240, A: 255})
	p.FillRoundRect(painter.Rect{X: 8, Y: 8, W: 80, H: 30}, 6, painter.RGBA{R: 10, G: 90, B: 200, A: 255})
	p.StrokeRoundRect(painter.Rect{X: 8, Y: 8, W: 80, H: 30}, 6, ink, 1)
	p.PushClip(painter.Rect{X: 4, Y: 4, W: 150, H: 100})
	p.PushTranslate(10, 12)
	p.Text(2, 2, "Button", ink)
	p.SetTextScale(2)
	p.Text(2, 14, "Big", ink)
	p.SetTextScale(1)
	p.PopTranslate()
	p.PopClip()
	for y := range 12 {
		for x := range 40 {
			p.PutPixel(100+x, 40+y, painter.RGBA{R: uint8(6 * x), G: uint8(20 * y), B: 128, A: 255})
		}
	}
	p.DrawMask(painter.Rect{X: 120, Y: 4, W: 4, H: 2}, []byte{0, 255, 128, 64, 255, 0, 64, 128}, 4, ink)
	p.DrawImage(painter.Rect{X: 130, Y: 60, W: 2, H: 2}, []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 9, 9, 9, 128}, 2, 2)
	p.Flush()
}

// TestResetEqualsNew: a painter that has been dirtied (translation and clip
// left open, text scale 3, pixels pending) and Reset records exactly what a
// new one records.
func TestResetEqualsNew(t *testing.T) {
	const w, h = 200, 120
	fresh, _ := gpu2d.NewList(w, h)
	frame(New(fresh))

	l, _ := gpu2d.NewList(w, h)
	p := New(l)
	p.PushTranslate(50, 50)
	p.SetTextScale(3)
	p.PutPixel(1, 1, painter.RGBA{R: 1, G: 2, B: 3, A: 255})
	p.PutPixel(2, 2, painter.RGBA{R: 1, G: 2, B: 3, A: 255})
	p.Text(0, 0, "x", painter.RGBA{A: 255})
	l.Reset()
	p.Reset(l)
	if p.TextScale() != 1 {
		t.Fatalf("text scale %d after Reset", p.TextScale())
	}
	frame(p)
	if !slices.Equal(l.Instances(), fresh.Instances()) || !bytes.Equal(l.Data(), fresh.Data()) {
		t.Fatalf("a reset painter recorded %d instances, a new one %d", len(l.Instances()), len(fresh.Instances()))
	}
	if p.List() != l {
		t.Fatal("List is not the list given to Reset")
	}
	// Pixels pending at Reset are dropped, not recorded into the new list.
	l.Reset()
	p.PutPixel(5, 5, painter.RGBA{A: 255})
	p.Reset(l)
	p.Flush()
	if n := len(l.Instances()); n != 0 {
		t.Fatalf("%d instances: pending pixels survived Reset", n)
	}
	// And the dropped pixel is not remembered as a duplicate.
	p.PutPixel(5, 5, painter.RGBA{A: 255})
	p.PutPixel(6, 5, painter.RGBA{A: 255})
	p.Flush()
	if n := len(l.Instances()); n != 1 {
		t.Fatalf("%d instances for a run of two pixels", n)
	}
}

// TestSteadyFrameAllocatesNothing: once the buffers have grown, a frame
// recorded through a reused painter into a reused list allocates nothing.
func TestSteadyFrameAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	l, _ := gpu2d.NewList(200, 120)
	p := New(l)
	run := func() {
		l.Reset()
		p.Reset(l)
		frame(p)
	}
	run()
	if n := testing.AllocsPerRun(10, run); n != 0 {
		t.Fatalf("a steady frame allocates %v times", n)
	}
	if got := len(l.Instances()); got < 8 {
		t.Fatalf("only %d instances: the frame barely draws", got)
	}
}

// BenchmarkFrame compares a painter made for every frame (what a caller
// without Reset must do) with one reused.
func BenchmarkFrame(b *testing.B) {
	b.Run("new", func(b *testing.B) {
		l, _ := gpu2d.NewList(200, 120)
		b.ReportAllocs()
		for b.Loop() {
			l.Reset()
			frame(New(l))
		}
	})
	b.Run("reset", func(b *testing.B) {
		l, _ := gpu2d.NewList(200, 120)
		p := New(l)
		b.ReportAllocs()
		for b.Loop() {
			l.Reset()
			p.Reset(l)
			frame(p)
		}
	})
}
