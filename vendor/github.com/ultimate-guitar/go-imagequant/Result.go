/*
Copyright (c) 2016, The go-imagequant author(s)

Permission to use, copy, modify, and/or distribute this software for any purpose
with or without fee is hereby granted, provided that the above copyright notice
and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND ISC DISCLAIMS ALL WARRANTIES WITH REGARD TO
THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS.
IN NO EVENT SHALL ISC BE LIABLE FOR ANY SPECIAL, DIRECT, INDIRECT, OR
CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA
OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS
ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS
SOFTWARE.
*/

package imagequant

import (
	"image/color"
	"unsafe"
)

/*
#include "libimagequant.h"
*/
import "C"

// Callers must not use this object once Release has been called on the parent
// Image struct.
type Result struct {
	p  *C.struct_liq_result
	im *Image
}

// Enables/disables dithering in liq_write_remapped_image(). Dithering level must be between 0 and 1 (inclusive).
// Dithering level 0 enables fast non-dithered remapping. Otherwise a variation of Floyd-Steinberg error diffusion is used.
// Precision of the dithering algorithm depends on the speed setting, see Attributes.SetSpeed()
func (this *Result) SetDitheringLevel(dither_level float32) error {
	return translateError(C.liq_set_dithering_level(this.p, C.float(dither_level)))
}

// Returns mean square error of quantization (square of difference between pixel values in the source image and its remapped version).
// Alpha channel, gamma correction and approximate importance of pixels is taken into account, so the result isn't exactly the mean square error of all channels.
// For most images MSE 1-5 is excellent. 7-10 is OK. 20-30 will have noticeable errors. 100 is awful.
// This function may return -1 if the value is not available (this happens when a high speed has been requested, the image hasn't been remapped yet, and quality limit hasn't been set, see Attributes.SetSpeed() and Attributes.SetQuality()).
// The value is not updated when multiple images are remapped, it applies only to the image used in liq_image_quantize() or the first image that has been remapped.
func (this *Result) GetQuantizationError() float64 {
	return float64(C.liq_get_quantization_error(this.p))
}

// Returns mean square error of last remapping done (square of difference between pixel values in the remapped image and its remapped version).
// Alpha channel and gamma correction are taken into account, so the result isn't exactly the mean square error of all channels.
// This function may return -1 if the value is not available (this happens when a high speed has been requested or the image hasn't been remapped yet).
func (this *Result) GetRemappingError() float64 {
	return float64(C.liq_get_remapping_error(this.p))
}

// Analoguous to Result.GetQuantizationError(), but returns quantization error as quality value in the same 0-100 range that is used by Attributes.SetQuality().
func (this *Result) GetQuantizationQuality() float64 {
	return float64(C.liq_get_quantization_quality(this.p))
}

// Analoguous to Result.GetRemappingError(), but returns quantization error as quality value in the same 0-100 range that is used by Attributes.SetQuality().
func (this *Result) GetRemappingQuality() float64 {
	return float64(C.liq_get_remapping_quality(this.p))
}

// Sets gamma correction for generated palette and remapped image.
// Must be > 0 and < 1, e.g. 0.45455 for gamma 1/2.2 in PNG images.
// By default output gamma is same as gamma of the input image.
func (this *Result) SetOutputGamma(gamma float64) error {
	return translateError(C.liq_set_output_gamma(this.p, C.double(gamma)))
}

func (this *Result) GetImageWidth() int {
	// C.liq_image_get_width
	return this.im.w
}

func (this *Result) GetImageHeight() int {
	// C.liq_image_get_height
	return this.im.h
}

func (this *Result) GetOutputGamma() float64 {
	return float64(C.liq_get_output_gamma(this.p))
}

// Remaps the image to palette and writes its pixels to the given []byte, 1 pixel per byte.
func (this *Result) WriteRemappedImage() ([]byte, error) {
	if this.im.released {
		return nil, ErrUseAfterFree
	}

	buff_size := this.im.w * this.im.h
	buff := make([]byte, buff_size)
	buffP := unsafe.Pointer(&buff[0])

	iqe := C.liq_write_remapped_image(this.p, this.im.p, buffP, C.size_t(buff_size))
	if iqe != C.LIQ_OK {
		return nil, translateError(iqe)
	}

	return buff, nil
}

// Returns color.Palette optimized for image that has been quantized or remapped (final refinements are applied to the palette during remapping).
// It's valid to call this method before remapping, if you don't plan to remap any images or want to use same palette for multiple images.
func (this *Result) GetPalette() color.Palette {
	ptr := C.liq_get_palette(this.p) // copy struct content
	max := int(ptr.count)

	ret := make([]color.Color, max)
	for i := 0; i < max; i += 1 {
		ret[i] = color.RGBA{
			R: uint8(ptr.entries[i].r),
			G: uint8(ptr.entries[i].g),
			B: uint8(ptr.entries[i].b),
			A: uint8(ptr.entries[i].a),
		}
	}

	return ret
}

// Free memory. Callers must not use this object after Release has been called.
func (this *Result) Release() {
	C.liq_result_destroy(this.p)
}
