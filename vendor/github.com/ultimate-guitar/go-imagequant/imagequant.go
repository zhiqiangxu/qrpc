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
#include "libimagequant.h"

const char* liqVersionString() {
	return LIQ_VERSION_STRING;
}

*/
import "C"

var (
	ErrQualityTooLow      = errors.New("Quality too low")
	ErrValueOutOfRange    = errors.New("Value out of range")
	ErrOutOfMemory        = errors.New("Out of memory")
	ErrAborted            = errors.New("Aborted")
	ErrBitmapNotAvailable = errors.New("Bitmap not available")
	ErrBufferTooSmall     = errors.New("Buffer too small")
	ErrInvalidPointer     = errors.New("Invalid pointer")

	ErrUseAfterFree = errors.New("Use after free")
)

func translateError(iqe C.liq_error) error {
	switch iqe {
	case C.LIQ_OK:
		return nil
	case (C.LIQ_QUALITY_TOO_LOW):
		return ErrQualityTooLow
	case (C.LIQ_VALUE_OUT_OF_RANGE):
		return ErrValueOutOfRange
	case (C.LIQ_OUT_OF_MEMORY):
		return ErrOutOfMemory
	case (C.LIQ_ABORTED):
		return ErrAborted
	case (C.LIQ_BITMAP_NOT_AVAILABLE):
		return ErrBitmapNotAvailable
	case (C.LIQ_BUFFER_TOO_SMALL):
		return ErrBufferTooSmall
	case (C.LIQ_INVALID_POINTER):
		return ErrInvalidPointer
	default:
		return errors.New("Unknown error")
	}
}

func GetLibraryVersion() int {
	return int(C.liq_version())
}

func GetLibraryVersionString() string {
	return C.GoString(C.liqVersionString())
}
