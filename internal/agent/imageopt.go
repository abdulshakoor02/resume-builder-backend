package agent

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // register the PNG decoder (stdlib); WebP is intentionally absent
	"log"
)

// maxModelImageEdge bounds the long edge of an image sent to the model, and
// maxModelImageBytes the size worth sending untouched.
//
// A design reference is usually a screenshot of a whole page, so it arrives as a
// large PNG. Sending it verbatim costs real time: the bytes are base64-encoded
// into the request (~1MB of base64 for a 718KB screenshot), the model has to
// prefill many vision tiles, and a measured design-referenced build took 38.5s
// against 6.5s for the same input without an image. Layout, palette and
// typography survive a downscale (they don't live in individual pixels), so a
// reference is shrunk before it is sent while the original bytes stay in storage.
const (
	maxModelImageEdge  = 768
	maxModelImageBytes = 400 * 1024
)

// ShrinkImageForModel returns a copy of data suitable to send to the model,
// re-encoded as JPEG when that is smaller, plus the MIME type to declare. It
// returns the input untouched when the image is already small, when the bytes
// can't be decoded (the standard library has no WebP decoder — better a big
// request than a broken one), or when re-encoding wouldn't help.
func ShrinkImageForModel(data []byte, mimeType string) ([]byte, string) {
	if len(data) == 0 {
		return data, mimeType
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return data, mimeType
	}
	longest := cfg.Width
	if cfg.Height > longest {
		longest = cfg.Height
	}
	if longest <= maxModelImageEdge && len(data) <= maxModelImageBytes {
		return data, mimeType
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, mimeType
	}
	scaled := boxDownscale(src, maxModelImageEdge)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: 82}); err != nil {
		return data, mimeType
	}
	out := buf.Bytes()
	if len(out) >= len(data) {
		return data, mimeType
	}
	b := scaled.Bounds()
	log.Printf("image: downscaled %dx%d %s (%d bytes) -> %dx%d jpeg (%d bytes) for the model call",
		cfg.Width, cfg.Height, mimeType, len(data), b.Dx(), b.Dy(), len(out))
	return out, "image/jpeg"
}

// boxDownscale resizes to at most maxEdge on the long edge by averaging each
// destination pixel's source block. Area averaging (rather than sampling single
// pixels) keeps the downscaled reference readable for the model.
func boxDownscale(src image.Image, maxEdge int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	longest := w
	if h > longest {
		longest = h
	}
	scale := float64(maxEdge) / float64(longest)
	nw, nh := int(float64(w)*scale), int(float64(h)*scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0 := b.Min.Y + y*h/nh
		y1 := b.Min.Y + (y+1)*h/nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < nw; x++ {
			x0 := b.Min.X + x*w/nw
			x1 := b.Min.X + (x+1)*w/nw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rs, gs, bs, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					r, g, bl, _ := src.At(xx, yy).RGBA()
					rs += uint64(r)
					gs += uint64(g)
					bs += uint64(bl)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(rs / n >> 8),
				G: uint8(gs / n >> 8),
				B: uint8(bs / n >> 8),
				A: 255,
			})
		}
	}
	return dst
}
