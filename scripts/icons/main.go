// Command icons renders Talaria's app icons (PNG) from an SVG wing mark.
//
//	go run ./scripts/icons
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

const wing = `<g fill="#ffffff">
  <path d="M120 372 C150 250 260 150 420 130 C380 190 330 230 260 270 C220 295 170 340 120 372 Z" opacity="0.96"/>
  <path d="M136 392 C190 320 290 270 410 250 C360 300 300 330 240 352 C200 366 165 384 136 392 Z" opacity="0.82"/>
  <path d="M152 408 C210 365 300 340 392 346 C340 375 290 390 236 400 C200 406 172 410 152 408 Z" opacity="0.66"/>
</g>`

const grad = `<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
  <stop offset="0" stop-color="#6457f5"/><stop offset="1" stop-color="#8d82f8"/></linearGradient></defs>`

func svg(rx int, wingTransform string, bg bool) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">`)
	b.WriteString(grad)
	if bg {
		fmt.Fprintf(&b, `<rect width="512" height="512" rx="%d" fill="url(#g)"/>`, rx)
	}
	if wingTransform != "" {
		fmt.Fprintf(&b, `<g transform="%s">%s</g>`, wingTransform, wing)
	} else {
		b.WriteString(wing)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func render(svgText string, size int, out string) error {
	icon, err := oksvg.ReadIconStream(strings.NewReader(svgText))
	if err != nil {
		return err
	}
	icon.SetTarget(0, 0, float64(size), float64(size))
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(size, size, scanner), 1.0)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func main() {
	dir := filepath.Join("web", "public", "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	standard := svg(112, "", true)
	// Maskable icons get cropped to a circle/squircle by the OS: keep the
	// mark inside the central 80% safe zone and square off the corners.
	maskable := svg(0, "translate(51 51) scale(0.8)", true)
	// Status-bar badge: white mark only, transparent background.
	badge := svg(0, "", false)

	must(os.WriteFile(filepath.Join(dir, "icon.svg"), []byte(standard), 0o644))
	must(render(standard, 192, filepath.Join(dir, "icon-192.png")))
	must(render(standard, 512, filepath.Join(dir, "icon-512.png")))
	must(render(standard, 180, filepath.Join(dir, "apple-touch-icon.png")))
	must(render(maskable, 512, filepath.Join(dir, "icon-maskable-512.png")))
	must(render(badge, 96, filepath.Join(dir, "badge-96.png")))
	fmt.Println("icons written to", dir)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
