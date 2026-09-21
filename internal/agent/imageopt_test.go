package agent

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// noisyPNG builds an image whose pixels vary pseudo-randomly, so the PNG cannot
// compress away the detail. A smooth gradient is a poor fixture here: it packs
// into a PNG smaller than the downscaled JPEG, and ShrinkImageForModel correctly
// keeps the original in that case.
func noisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(12345)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223
			img.Set(x, y, color.RGBA{uint8(seed >> 24), uint8(seed >> 16), uint8(seed >> 8), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestShrinkImageForModel(t *testing.T) {
	// A page-sized screenshot shrinks, bounded on its long edge and re-encoded.
	big := noisyPNG(t, 1600, 2000)
	out, mime := ShrinkImageForModel(big, "image/png")
	if mime != "image/jpeg" {
		t.Fatalf("large image should become jpeg, got %s", mime)
	}
	if len(out) >= len(big) {
		t.Fatalf("expected a smaller payload, got %d -> %d bytes", len(big), len(out))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output is not a decodable image: %v", err)
	}
	if cfg.Width > maxModelImageEdge || cfg.Height > maxModelImageEdge {
		t.Fatalf("long edge not bounded: %dx%d", cfg.Width, cfg.Height)
	}
	t.Logf("large image: %d -> %d bytes, %dx%d", len(big), len(out), cfg.Width, cfg.Height)

	// An already-small image is passed through byte-for-byte.
	small := solidPNG(t, 240, 300)
	if o, m := ShrinkImageForModel(small, "image/png"); m != "image/png" || !bytes.Equal(o, small) {
		t.Fatalf("small image must pass through untouched (mime=%s, equal=%v)", m, bytes.Equal(o, small))
	}

	// Undecodable bytes (e.g. WebP, which the stdlib cannot read) pass through:
	// a big request beats a broken one.
	raw := []byte("not an image at all")
	if o, m := ShrinkImageForModel(raw, "image/webp"); m != "image/webp" || !bytes.Equal(o, raw) {
		t.Fatalf("undecodable input must pass through untouched (mime=%s)", m)
	}
}

// TestShrinkSample measures a real reference when SHRINK_SAMPLE points at one -
// used to check the saving on an actual user-supplied screenshot.
func TestShrinkSample(t *testing.T) {
	path := os.Getenv("SHRINK_SAMPLE")
	if path == "" {
		t.Skip("SHRINK_SAMPLE not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	out, mime := ShrinkImageForModel(data, "image/png")
	t.Logf("sample %s: %d bytes -> %d bytes (%s), %.1f%% of the original payload",
		path, len(data), len(out), mime, 100*float64(len(out))/float64(len(data)))
}
