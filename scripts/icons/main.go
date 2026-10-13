// Command icons renders Talaria's app icons (PNG) from an SVG wing mark.
//
//	go run ./scripts/icons
package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	xdraw "golang.org/x/image/draw"
)

const wing = `<g fill="#ffffff">
  <path d="M120 372 C150 250 260 150 420 130 C380 190 330 230 260 270 C220 295 170 340 120 372 Z" opacity="0.96"/>
  <path d="M136 392 C190 320 290 270 410 250 C360 300 300 330 240 352 C200 366 165 384 136 392 Z" opacity="0.82"/>
  <path d="M152 408 C210 365 300 340 392 346 C340 375 290 390 236 400 C200 406 172 410 152 408 Z" opacity="0.66"/>
</g>`

const grad = `<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
  <stop offset="0" stop-color="#6457f5"/><stop offset="1" stop-color="#8d82f8"/></linearGradient></defs>`

// svg composes the mark: optional gradient background (with corner radius)
// and optional wing. Transforms are deliberately avoided; the rasterizer
// used here drops transformed groups, so scaling is done in pixels instead.
func svg(rx int, withBG, withWing bool) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">`)
	b.WriteString(grad)
	if withBG {
		fmt.Fprintf(&b, `<rect width="512" height="512" rx="%d" fill="url(#g)"/>`, rx)
	}
	if withWing {
		b.WriteString(wing)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func rasterize(svgText string, size int) (*image.RGBA, error) {
	icon, err := oksvg.ReadIconStream(strings.NewReader(svgText))
	if err != nil {
		return nil, err
	}
	icon.SetTarget(0, 0, float64(size), float64(size))
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(size, size, scanner), 1.0)
	return img, nil
}

func save(img image.Image, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func render(svgText string, size int, out string) error {
	img, err := rasterize(svgText, size)
	if err != nil {
		return err
	}
	return save(img, out)
}

// maskable draws the wing at 80% inside a full-bleed square: launchers crop
// maskable icons to circles/squircles, so the mark must stay in the centre.
func maskable(size int, out string) error {
	bg, err := rasterize(svg(0, true, false), size)
	if err != nil {
		return err
	}
	wingImg, err := rasterize(svg(0, false, true), size)
	if err != nil {
		return err
	}
	inner := size * 80 / 100
	off := (size - inner) / 2
	dst := image.Rect(off, off, off+inner, off+inner)
	xdraw.CatmullRom.Scale(bg, dst, wingImg, wingImg.Bounds(), draw.Over, nil)
	return save(bg, out)
}

func main() {
	dir := filepath.Join("web", "public", "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	standard := svg(112, true, true)
	badge := svg(0, false, true) // white mark, transparent background
	must(os.WriteFile(filepath.Join(dir, "icon.svg"), []byte(standard), 0o644))
	must(render(standard, 192, filepath.Join(dir, "icon-192.png")))
	must(render(standard, 512, filepath.Join(dir, "icon-512.png")))
	must(render(standard, 180, filepath.Join(dir, "apple-touch-icon.png")))
	must(maskable(512, filepath.Join(dir, "icon-maskable-512.png")))
	must(render(badge, 96, filepath.Join(dir, "badge-96.png")))
	fmt.Println("icons written to", dir)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
