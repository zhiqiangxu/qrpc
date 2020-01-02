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

package main

import (
	"flag"
	"fmt"
	"github.com/ultimate-guitar/go-imagequant"
	"image/png"
	"io/ioutil"
	"os"
)

func crushFile(sourcefile, destfile string, speed int, compression png.CompressionLevel) error {

	sourceFh, err := os.OpenFile(sourcefile, os.O_RDONLY, 0444)
	if err != nil {
		return fmt.Errorf("os.OpenFile: %s", err.Error())
	}
	defer sourceFh.Close()

	image, err := ioutil.ReadAll(sourceFh)
	if err != nil {
		return fmt.Errorf("ioutil.ReadAll: %s", err.Error())
	}

	optiImage, err := imagequant.Crush(image, speed, compression)
	if err != nil {
		return fmt.Errorf("imagequant.Crush: %s", err.Error())
	}

	destFh, err := os.OpenFile(destfile, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("os.OpenFile: %s", err.Error())
	}
	defer destFh.Close()

	destFh.Write(optiImage)
	return nil
}

func main() {
	ShouldDisplayVersion := flag.Bool("Version", false, "")
	Infile := flag.String("In", "", "Input filename")
	Outfile := flag.String("Out", "", "Output filename")
	Speed := flag.Int("Speed", 3, "Speed (1 slowest, 10 fastest)")
	Compression := flag.Int("Compression", -3, "Compression level (DefaultCompression = 0, NoCompression = -1, BestSpeed = -2, BestCompression = -3)")

	flag.Parse()

	if *ShouldDisplayVersion {
		fmt.Printf("libimagequant '%s' (%d)\n", imagequant.GetLibraryVersionString(), imagequant.GetLibraryVersion())
		os.Exit(0)
	}

	var cLevel png.CompressionLevel
	switch *Compression {
	case 0:
		cLevel = png.DefaultCompression
	case -1:
		cLevel = png.NoCompression
	case -2:
		cLevel = png.BestSpeed
	case -3:
		cLevel = png.BestCompression
	default:
		cLevel = png.BestCompression
	}

	err := crushFile(*Infile, *Outfile, *Speed, cLevel)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	os.Exit(0)
}
