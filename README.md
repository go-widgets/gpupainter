# gpupainter

A go-widgets `painter.Painter` for the GPU. Widgets draw into it as they draw
into `PixelPainter`. It records their calls into a
[go-gfx/gpu2d](https://github.com/go-gfx/gpu2d) list, and a GPU back-end draws
that list in one submission: `gpu2d/webgpu` in a browser, or `gpu2d/metal` on
a Mac. Pure Go, no cgo.

```go
l, _ := gpu2d.NewList(w, h)
p := gpupainter.New(l)
root.Draw(p, theme)            // any go-widgets widget tree

r, _ := metal.New(w, h)        // or webgpu.New in a browser
copy(r.Pixels(), background)
_ = r.Draw(l)                  // the frame, on the GPU
l.Reset()                      // next frame
```

## One painter for every frame

`New` is cheap, but a painter's scratch (the glyph surface text is drawn
into, the patch a run of pixels becomes, the pixel index) grows to its working
size on the first frame. A painter made per frame pays that again each time:
on the gallery frame, 3.2 MB per frame. Keep one and `Reset` it:

```go
l, _ := gpu2d.NewList(w, h)
p := gpupainter.New(l)
for {
	l.Reset()
	p.Reset(l)                 // no translation, text scale 1, pixels dropped; scratch kept
	root.Draw(p, theme)
	_ = r.Draw(p.List())
}
```

`Reset(l)` does not reset `l`. A steady frame through a reused painter and a
reused list allocates nothing (`TestSteadyFrameAllocatesNothing`, and
`BenchmarkFrame`: 31 allocs, 84 KB and 28.7 µs per frame with `New`; 0
allocs and 10.0 µs with `Reset`, medians of 8 interleaved rounds on a small
widget frame, M4 Max, load about 7). Only a path drawn under a translation still builds a
shifted copy of the path.

## Same pixels as PixelPainter

`Painter` implements every capability `PixelPainter` has:

- `Clipper` and `Translator`;
- `MaskPainter` (glyphs), `ImagePainter` and `PathPainter`;
- the built-in 5 × 7 font, with `SetTextScale`, `TextWidth` and `TextHeight`.

`TestMatchesPixelPainter` draws 2000 random scenes with both painters and
compares every byte of the result through gpu2d's CPU reference. The scenes
include text at scales 1 to 3, single pixels, masks, scaled images, and filled
and stroked paths with lines, quadratics and cubics, under nested clips and
translations. On the GPU, gpu2d's Metal back-end equals that reference on
every channel. Its WebGPU back-end does too over an opaque surface, which is
what a window is.

How each primitive is recorded:

- **Text:** the built-in font lights each glyph bit with `PutPixel`, or an
  n × n fill when magnified. The string is therefore drawn once in white into
  a scratch buffer and sent as one coverage mask in the ink, which blends
  exactly as `PutPixel` would.
- **Paths:** rasterized by the same go-gfx/gfx/vector rasterizer
  `PixelPainter` uses, and moved by the translation in force, as
  `PixelPainter` does since go-widgets/painter v0.16.2.

## Pixels painted one by one

A widget that paints pixel by pixel, such as a colour picker's square, would
otherwise cost one instance per pixel. In the go-widgets gallery that was
25,181 of 28,753 instances.

`PixelPainter` blends each `PutPixel` onto the ground on its own. So a run of
them at distinct positions, under one clip and translation, is recorded as one
image whose untouched texels are transparent, and it blends identically. The
run is flushed before any other call, when a pixel would land twice, and when
its box would pass 2^20 pixels. This takes the gallery to 3,733 instances,
still byte-identical.

**Call `Flush`, or take the list with `List`, before handing the list to a
renderer.** Otherwise the last run of pixels is still pending and is not drawn.

## Speed

From go-gfx/gpu2d on an Apple M4 Max, for a widget-toolkit frame:

| frame | PixelPainter | WebGPU | Metal |
|---|---:|---:|---:|
| 1080p | 13.1 ms | 0.99 ms | 1.08 ms |
| 4K | 52.5 ms | 1.62 ms | 2.79 ms |

**Where it does not pay:** the go-widgets gallery as it ships is 960 × 2834
pixels at 1× scale. On the same machine it takes 1.98 ms on `PixelPainter`,
and less than half of that is rasterising; the rest is the widgets' own
logic, which both paths run. Through this painter and Metal it took about
3.5 ms (load about 8, so indicative): 1.7 ms of recording, widget logic
included; 0.26 ms of binning; and 1.7 ms for the GPU draw with its upload
and synchronous wait.

The GPU wins when pixels dominate: large surfaces, 4K or HiDPI repaints,
images and animation. A small frame at 1× is already cheap on the CPU.

## Licence

BSD 3-Clause, see [LICENSE](LICENSE).
