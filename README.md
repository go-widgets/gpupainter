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

## Speed

From go-gfx/gpu2d on an Apple M4 Max, for a widget-toolkit frame:

| frame | PixelPainter | WebGPU | Metal |
|---|---:|---:|---:|
| 1080p | 13.1 ms | 0.99 ms | 1.08 ms |
| 4K | 52.5 ms | 1.62 ms | 2.79 ms |

## Licence

BSD 3-Clause, see [LICENSE](LICENSE).
