//go:build ignore

package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
)

func main() {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 235, G: 238, B: 242, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(24, 24, 296, 216), &image.Uniform{C: color.RGBA{R: 48, G: 112, B: 170, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(74, 70, 124, 120), &image.Uniform{C: color.RGBA{R: 245, G: 180, B: 65, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(180, 105, 245, 170), &image.Uniform{C: color.RGBA{R: 80, G: 165, B: 105, A: 255}}, image.Point{}, draw.Src)
	file, err := os.Create("testdata/seed.jpg")
	if err != nil {
		panic(err)
	}
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 80}); err != nil {
		panic(err)
	}
	if err := file.Close(); err != nil {
		panic(err)
	}
}
