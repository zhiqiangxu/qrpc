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
	"errors"
)

/*
#cgo CFLAGS: -I${SRCDIR}/libimagequant -std=c99
#cgo LDFLAGS: -lm
#include "blur.c"
#include "kmeans.c"
#include "libimagequant.c"
#include "mediancut.c"
#include "mempool.c"
#include "nearest.c"
#include "pam.c"
#include "libimagequant.h"
*/
import "C"

type Attributes struct {
	p *C.struct_liq_attr
}

// Returns object that will hold initial settings (attributes) for the library.
// Callers MUST call Release() on the returned object to free memory.
func NewAttributes() (*Attributes, error) {
	pAttr := C.liq_attr_create()
	if pAttr == nil { // nullptr
		return nil, errors.New("Unsupported platform")
	}

	return &Attributes{p: pAttr}, nil
}

const (
	COLORS_MIN = 2
	COLORS_MAX = 256
)

// Specifies maximum number of colors to use. The default is 256.
// Instead of setting a fixed limit it's better to use  Attributes.SetQuality()
func (this *Attributes) SetMaxColors(colors int) error {
	return translateError(C.liq_set_max_colors(this.p, C.int(colors)))
}

func (this *Attributes) GetMaxColors() int {
	return int(C.liq_get_max_colors(this.p))
}

const (
	QUALITY_MIN = 0
	QUALITY_MAX = 100
)

// Quality is in range 0 (worst) to 100 (best) and values are analoguous to JPEG quality (i.e. 80 is usually good enough).
// Quantization will attempt to use the lowest number of colors needed to achieve maximum quality. maximum value of 100 is the default and means conversion as good as possible.
// If it's not possible to convert the image with at least minimum quality (i.e. 256 colors is not enough to meet the minimum quality), then Image.Quantize() will fail. The default minimum is 0 (proceeds regardless of quality).
//
// Features dependent on speed:
// speed 1-5: Noise-sensitive dithering
// speed 8-10 or if image has more than million colors: Forced posterization
// speed 1-7 or if minimum quality is set: Quantization error known
// seed 1-6: Additional quantization techniques
func (this *Attributes) SetQuality(minimum, maximum int) error {
	return translateError(C.liq_set_quality(this.p, C.int(minimum), C.int(maximum)))
}

func (this *Attributes) GetMinQuality() int {
	return int(C.liq_get_min_quality(this.p))
}

func (this *Attributes) GetMaxQuality() int {
	return int(C.liq_get_max_quality(this.p))
}

const (
	SPEED_SLOWEST = 1
	SPEED_DEFAULT = 3
	SPEED_FASTEST = 10
)

// Higher speed levels disable expensive algorithms and reduce quantization precision. The default speed is 3. Speed 1 gives marginally better quality at significant CPU cost. Speed 10 has usually 5% lower quality, but is 8 times faster than the default.
// High speeds combined with Attributes.SetQuality() will use more colors than necessary and will be less likely to meet minimum required quality.
func (this *Attributes) SetSpeed(speed int) error {
	return translateError(C.liq_set_speed(this.p, C.int(speed)))
}

func (this *Attributes) GetSpeed() int {
	return int(C.liq_get_speed(this.p))
}

// Alpha values higher than this will be rounded to opaque. This is a workaround for Internet Explorer 6, but because this browser is not used any more, this option is deprecated and will be removed.
// The default is 255 (no change).
func (this *Attributes) SetMinOpacity(min int) error {
	return translateError(C.liq_set_min_opacity(this.p, C.int(min)))
}

func (this *Attributes) GetMinOpacity() int {
	return int(C.liq_get_min_opacity(this.p))
}

// Ignores given number of least significant bits in all channels, posterizing image to 2^bits levels.
// 0 gives full quality.
// Use 2 for VGA or 16-bit RGB565 displays.
// 4 if image is going to be output on a RGB444/RGBA4444 display (e.g. low-quality textures on Android).
func (this *Attributes) SetMinPosterization(bits int) error {
	return translateError(C.liq_set_min_posterization(this.p, C.int(bits)))
}

func (this *Attributes) GetMinPosterization() int {
	return int(C.liq_get_min_posterization(this.p))
}

// 0 (default) makes alpha colors sorted before opaque colors.
// Non-0 mixes colors together except completely transparent color, which is moved to the end of the palette.
// This is a workaround for programs that blindly assume the last palette entry is transparent.
func (this *Attributes) SetLastIndexTransparent(is_last int) {
	C.liq_set_last_index_transparent(this.p, C.int(is_last))
}

// Creates histogram object that will be used to collect color statistics from multiple images.
// It must be freed using Histogram.Release()
func (this *Attributes) CreateHistogram() *Histogram {
	ptr := C.liq_histogram_create(this.p)
	return &Histogram{p: ptr}
}

// Free memory. Callers must not use this object after Release has been called.
func (this *Attributes) Release() {
	C.liq_attr_destroy(this.p)
}
