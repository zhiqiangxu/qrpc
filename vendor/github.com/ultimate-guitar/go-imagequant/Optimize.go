package imagequant

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// ImageToRgba32 gets the image struct and return []byte where each pixel encoded to 4 byte (R+G+B+A)
func ImageToRgba32(im image.Image) (ret []byte) {
	w := im.Bounds().Max.X
	h := im.Bounds().Max.Y
	ret = make([]byte, w*h*4)

	p := 0

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r16, g16, b16, a16 := im.At(x, y).RGBA() // Each value ranges within [0, 0xffff]

			ret[p+0] = uint8(r16 >> 8)
			ret[p+1] = uint8(g16 >> 8)
			ret[p+2] = uint8(b16 >> 8)
			ret[p+3] = uint8(a16 >> 8)
			p += 4
		}
	}
	return ret
}

func Rgb8PaletteToGoImage(w, h int, rgb8data []byte, pal color.Palette) image.Image {
	rect := image.Rectangle{
		Max: image.Point{
			X: w,
			Y: h,
		},
	}

	ret := image.NewPaletted(rect, pal)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ret.SetColorIndex(x, y, rgb8data[y*w+x])
		}
	}
	return ret
}

// Crush gets PNG image as []byte, speed int (from 1 to 10, 1 is slowest) and return optimized PNG image as []byte and error.
// In most cases you should use this function for image optimization.
func Crush(image []byte, speed int, compression png.CompressionLevel) (out []byte, err error) {
	reader := bytes.NewReader(image)
	img, err := png.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("png.Decode: %s", err.Error())
	}

	width := img.Bounds().Max.X
	height := img.Bounds().Max.Y

	attr, err := NewAttributes()
	if err != nil {
		return nil, fmt.Errorf("NewAttributes: %s", err.Error())
	}
	defer attr.Release()

	err = attr.SetSpeed(speed)
	if err != nil {
		return nil, fmt.Errorf("SetSpeed: %s", err.Error())
	}

	rgba32data := string(ImageToRgba32(img))

	iqm, err := NewImage(attr, rgba32data, width, height, 0)
	if err != nil {
		return nil, fmt.Errorf("NewImage: %s", err.Error())
	}
	defer iqm.Release()

	res, err := iqm.Quantize(attr)
	if err != nil {
		return nil, fmt.Errorf("Quantize: %s", err.Error())
	}
	defer res.Release()

	rgb8data, err := res.WriteRemappedImage()
	if err != nil {
		return nil, fmt.Errorf("WriteRemappedImage: %s", err.Error())
	}

	im2 := Rgb8PaletteToGoImage(res.GetImageWidth(), res.GetImageHeight(), rgb8data, res.GetPalette())

	writer := bytes.NewBuffer(out)
	encoder := &png.Encoder{CompressionLevel: compression}

	err = encoder.Encode(writer, im2)
	if err != nil {
		return nil, fmt.Errorf("png.Encode: %s", err.Error())
	}
	out = writer.Bytes()

	return out, nil
}
