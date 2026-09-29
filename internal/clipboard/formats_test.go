package clipboard

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"testing"
	"unicode/utf16"
)

func TestUTF16ClipboardText(t *testing.T) {
	text := "[~/.img2ssh/img2.png] " + string(rune(0x1f4a1))
	got := utf16ClipboardText(text)
	if len(got) == 0 || got[len(got)-1] != 0 {
		t.Fatal("CF_UNICODETEXT payload is missing its NUL terminator")
	}
	if decoded := string(utf16.Decode(got[:len(got)-1])); decoded != text {
		t.Fatalf("UTF-16 round trip got %q, want %q", decoded, text)
	}
}

func TestDIBRoundTrip(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 25, B: 10, A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{R: 4, G: 160, B: 80, A: 128})
	source.SetNRGBA(0, 1, color.NRGBA{R: 30, G: 60, B: 90, A: 255})
	source.SetNRGBA(1, 1, color.NRGBA{R: 200, G: 210, B: 220, A: 255})
	pngData := encodeTestPNG(t, source)

	for _, encode := range []struct {
		name string
		fn   func([]byte) ([]byte, error)
	}{
		{name: "DIB", fn: encodeDIB},
		{name: "DIBV5", fn: encodeDIBV5},
	} {
		t.Run(encode.name, func(t *testing.T) {
			data, err := encode.fn(pngData)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeDIB(data)
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < 2; y++ {
				for x := 0; x < 2; x++ {
					want := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
					actual := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
					if encode.name == "DIB" {
						want.A = 255
					}
					if actual != want {
						t.Errorf("pixel (%d,%d): got %+v, want %+v", x, y, actual, want)
					}
				}
			}
		})
	}
}

func TestDecodeDIBRejectsTruncatedPixels(t *testing.T) {
	data := make([]byte, 40)
	putDIBHeader(data, 2, 2, 40, 0)
	if _, err := decodeDIB(data); err == nil {
		t.Fatal("expected truncated DIB to be rejected")
	}
}

func TestEncodeDIBRejectsInvalidPNG(t *testing.T) {
	if _, err := encodeDIB([]byte("not a PNG")); err == nil {
		t.Fatal("expected invalid PNG to be rejected")
	}
}

func TestDecodeDIBV5AlphaMask(t *testing.T) {
	data := make([]byte, 128)
	putDIBHeader(data[:40], 1, -1, 124, 3)
	data[40] = 0x00
	data[41] = 0x00
	data[42] = 0xff
	data[43] = 0x00
	data[44] = 0x00
	data[45] = 0xff
	data[48] = 0xff
	data[55] = 0xff
	data[56] = 0x42
	data[57] = 0x47
	data[58] = 0x52
	data[59] = 0x73
	data[124] = 11 // B
	data[125] = 22 // G
	data[126] = 33 // R
	data[127] = 44 // A

	img, err := decodeDIB(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA), (color.NRGBA{R: 33, G: 22, B: 11, A: 44}); got != want {
		t.Fatalf("decoded V5 pixel = %+v, want %+v", got, want)
	}
}

func encodeTestPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestDIBEncodingStable(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	data := encodeTestPNG(t, img)
	dib1, err := encodeDIB(data)
	if err != nil {
		t.Fatal(err)
	}
	dib2, err := encodeDIB(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dib1, dib2) {
		t.Fatal("encoding the same PNG produced different DIB data")
	}
}
