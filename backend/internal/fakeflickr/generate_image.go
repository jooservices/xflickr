package fakeflickr

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
)

// generatedJPEG creates stable, photo-specific image content for download and deduplication flows.
func generatedJPEG(photo Photo) []byte {
	seed := sha256.Sum256([]byte(photo.ID + "\x00" + photo.Shape))
	palette := [][3]uint8{{52, 112, 170}, {176, 82, 91}, {67, 143, 104}, {133, 94, 168}, {198, 133, 52}}
	bg, accent := palette[int(seed[0])%len(palette)], palette[int(seed[1])%len(palette)]
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 238, G: 239, B: 233, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(18, 18, 302, 222), &image.Uniform{C: color.RGBA{R: bg[0], G: bg[1], B: bg[2], A: 255}}, image.Point{}, draw.Src)
	mark := color.RGBA{R: accent[0], G: accent[1], B: accent[2], A: 255}
	switch photo.Shape {
	case "landscape":
		draw.Draw(img, image.Rect(18, 115, 302, 222), &image.Uniform{C: mark}, image.Point{}, draw.Src)
		for x := 38; x < 282; x += 30 {
			y := 45 + int(seed[x%len(seed)]%35)
			draw.Draw(img, image.Rect(x, y, x+20, 115), &image.Uniform{C: color.RGBA{R: 244, G: 195, B: 88, A: 255}}, image.Point{}, draw.Src)
		}
	case "geometry":
		for i := 0; i < 7; i++ {
			x, y := 28+int(seed[i])%240, 28+int(seed[i+8])%160
			w, h := 24+int(seed[i+16])%48, 20+int(seed[i+24])%45
			draw.Draw(img, image.Rect(x, y, min(300, x+w), min(220, y+h)), &image.Uniform{C: mark}, image.Point{}, draw.Src)
		}
	case "color":
		for y := 18; y < 222; y += 26 {
			c := palette[(y/26+int(seed[2]))%len(palette)]
			draw.Draw(img, image.Rect(18, y, 302, min(222, y+26)), &image.Uniform{C: color.RGBA{R: c[0], G: c[1], B: c[2], A: 255}}, image.Point{}, draw.Src)
		}
	default:
		cx, cy := 160+int(seed[2])%45-22, 120+int(seed[3])%35-17
		for y := max(18, cy-72); y < min(222, cy+72); y++ {
			for x := max(18, cx-72); x < min(302, cx+72); x++ {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy < 72*72 {
					img.Set(x, y, mark)
				}
			}
		}
	}
	var out bytes.Buffer
	_ = jpeg.Encode(&out, img, &jpeg.Options{Quality: 82})
	return out.Bytes()
}
