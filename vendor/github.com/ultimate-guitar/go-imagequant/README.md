# Go-ImageQuant
[![Go Report Card](https://goreportcard.com/badge/github.com/ultimate-guitar/go-imagequant)](https://goreportcard.com/report/github.com/ultimate-guitar/go-imagequant) [![Build Status](https://travis-ci.org/ultimate-guitar/go-imagequant.svg?branch=master)](https://travis-ci.org/ultimate-guitar/go-imagequant) [![GoDoc](https://godoc.org/github.com/ultimate-guitar/go-imagequant?status.svg)](https://godoc.org/github.com/ultimate-guitar/go-imagequant)
## ABOUT
This is Go bindings for libimagequant.

Libimagequant is the backend of pngquant app. It provides a high level of png image compression.

Libimagequant is a library for lossy recompression of PNG images to reduce their filesize.  This go-imagequant project is a set of bindings for libimagequant to enable its use from the Go programming language.

This binding was written by hand. The result is somewhat more idiomatic than an automated conversion, but some  defer foo.Release() calls are required for memory management.

This project forked from [https://code.ivysaur.me/go-imagequant/](https://code.ivysaur.me/go-imagequant/) repo.

## USAGE
Usage example is provided by a sample utility cmd/gopngquant which mimics some functionality of the upstream pngquant.

The sample utility has the following options:

```
Usage of gopngquant:
  -In string
        Input filename
  -Out string
        Output filename
  -Speed int
        Speed (1 slowest, 10 fastest) (default: 3)
  -Compression int
        Compression level (DefaultCompression = 0, NoCompression = -1, BestSpeed = -2, BestCompression = -3) (default: BestCompression )
  -Version
```

## BUILDING

Install a C11 compiler and simply `go get github.com/ultimate-guitar/go-imagequant`.

## LICENSE
I am releasing this binding under the ISC license, however, libimagequant itself is released under GPLv3-or-later and/or commercial licenses. You must comply with the terms of such a license when using this binding in a Go project.

## CHANGELOG

See [CHANGELOG](CHANGELOG.md).
