package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"unicode/utf16"
)

const maxDIBPixels = 32 * 1024 * 1024

// utf16ClipboardText returns a Windows CF_UNICODETEXT payload, including the
// required UTF-16 NUL terminator. Keeping the encoding here makes it testable
// without a Windows clipboard session.
func utf16ClipboardText(text string) []uint16 {
	encoded := utf16.Encode([]rune(text))
	return append(encoded, 0)
}

// encodeDIB creates a 32-bit BI_RGB DIB for consumers that do not understand
// PNG clipboard formats. The image is stored bottom-up as required by classic
// DIBs.
func encodeDIB(pngData []byte) ([]byte, error) {
	img, err := decodePNGForDIB(pngData)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if err := validDIBDimensions(width, height); err != nil {
		return nil, err
	}
	stride := width * 4
	data := make([]byte, 40+stride*height)
	putDIBHeader(data[:40], width, height, 40, 0)
	for y := 0; y < height; y++ {
		row := data[40+(height-1-y)*stride : 40+(height-y)*stride]
		for x := 0; x < width; x++ {
			c := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			row[x*4+0] = c.B
			row[x*4+1] = c.G
			row[x*4+2] = c.R
			row[x*4+3] = 0xff
		}
	}
	return data, nil
}

// encodeDIBV5 creates a top-down 32-bit sRGB BITMAPV5HEADER payload with an
// explicit alpha mask. It is used alongside PNG and CF_DIB for native Windows
// image applications.
func encodeDIBV5(pngData []byte) ([]byte, error) {
	img, err := decodePNGForDIB(pngData)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if err := validDIBDimensions(width, height); err != nil {
		return nil, err
	}
	stride := width * 4
	data := make([]byte, 124+stride*height)
	putDIBHeader(data[:40], width, -height, 124, 3)
	binary.LittleEndian.PutUint32(data[20:24], uint32(stride*height))
	binary.LittleEndian.PutUint32(data[40:44], 0x00ff0000) // red
	binary.LittleEndian.PutUint32(data[44:48], 0x0000ff00) // green
	binary.LittleEndian.PutUint32(data[48:52], 0x000000ff) // blue
	binary.LittleEndian.PutUint32(data[52:56], 0xff000000) // alpha
	binary.LittleEndian.PutUint32(data[56:60], 0x73524742) // LCS_sRGB
	binary.LittleEndian.PutUint32(data[108:112], 4)        // LCS_GM_IMAGES
	for y := 0; y < height; y++ {
		row := data[124+y*stride : 124+(y+1)*stride]
		for x := 0; x < width; x++ {
			c := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			row[x*4+0] = c.B
			row[x*4+1] = c.G
			row[x*4+2] = c.R
			row[x*4+3] = c.A
		}
	}
	return data, nil
}

func decodePNGForDIB(data []byte) (image.Image, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode clipboard PNG header: %w", err)
	}
	if err := validDIBDimensions(config.Width, config.Height); err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode clipboard PNG: %w", err)
	}
	return img, nil
}

func putDIBHeader(header []byte, width, height, headerSize, compression int) {
	binary.LittleEndian.PutUint32(header[0:4], uint32(headerSize))
	binary.LittleEndian.PutUint32(header[4:8], uint32(int32(width)))
	binary.LittleEndian.PutUint32(header[8:12], uint32(int32(height)))
	binary.LittleEndian.PutUint16(header[12:14], 1)
	binary.LittleEndian.PutUint16(header[14:16], 32)
	binary.LittleEndian.PutUint32(header[16:20], uint32(compression))
}

func validDIBDimensions(width, height int) error {
	if width <= 0 || height == 0 {
		return errors.New("invalid DIB dimensions")
	}
	absHeight := int64(height)
	if absHeight < 0 {
		absHeight = -absHeight
	}
	if absHeight <= 0 || int64(width) > maxDIBPixels/absHeight {
		return errors.New("DIB image is too large")
	}
	return nil
}

// decodeDIB decodes the unframed DIB byte block returned for CF_DIB or
// CF_DIBV5. It supports the uncompressed formats emitted by common Windows
// screenshot and image applications.
func decodeDIB(data []byte) (image.Image, error) {
	if len(data) < 40 {
		return nil, errors.New("DIB header is truncated")
	}
	headerSize := int(binary.LittleEndian.Uint32(data[:4]))
	if headerSize < 40 || headerSize > len(data) {
		return nil, errors.New("unsupported DIB header")
	}
	width32 := int32(binary.LittleEndian.Uint32(data[4:8]))
	height32 := int32(binary.LittleEndian.Uint32(data[8:12]))
	if width32 <= 0 || height32 == 0 || height32 == math.MinInt32 {
		return nil, errors.New("invalid DIB dimensions")
	}
	width := int(width32)
	height := int(height32)
	if height < 0 {
		height = -height
	}
	if err := validDIBDimensions(width, height); err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint16(data[12:14]) != 1 {
		return nil, errors.New("invalid DIB plane count")
	}
	bitCount := int(binary.LittleEndian.Uint16(data[14:16]))
	compression := binary.LittleEndian.Uint32(data[16:20])
	if compression != 0 && compression != 3 && compression != 6 {
		return nil, errors.New("unsupported compressed DIB")
	}
	switch bitCount {
	case 1, 4, 8, 16, 24, 32:
	default:
		return nil, errors.New("unsupported DIB bit depth")
	}
	if compression != 0 && bitCount != 16 && bitCount != 32 {
		return nil, errors.New("invalid bitfield DIB depth")
	}

	offset := headerSize
	var redMask, greenMask, blueMask, alphaMask uint32
	if compression == 3 || compression == 6 {
		maskOffset := 40
		if headerSize == 40 {
			extraMasks := 12
			if compression == 6 {
				extraMasks = 16
			}
			if len(data) < headerSize+extraMasks {
				return nil, errors.New("DIB bit masks are truncated")
			}
			redMask = binary.LittleEndian.Uint32(data[maskOffset : maskOffset+4])
			greenMask = binary.LittleEndian.Uint32(data[maskOffset+4 : maskOffset+8])
			blueMask = binary.LittleEndian.Uint32(data[maskOffset+8 : maskOffset+12])
			if compression == 6 {
				alphaMask = binary.LittleEndian.Uint32(data[maskOffset+12 : maskOffset+16])
			}
			offset += extraMasks
		} else {
			if headerSize < 52 {
				return nil, errors.New("DIB bit masks are missing")
			}
			redMask = binary.LittleEndian.Uint32(data[40:44])
			greenMask = binary.LittleEndian.Uint32(data[44:48])
			blueMask = binary.LittleEndian.Uint32(data[48:52])
			if headerSize >= 56 {
				alphaMask = binary.LittleEndian.Uint32(data[52:56])
			}
		}
	} else if bitCount == 16 {
		redMask, greenMask, blueMask = 0x7c00, 0x03e0, 0x001f
	} else if bitCount == 32 && headerSize >= 56 {
		alphaMask = binary.LittleEndian.Uint32(data[52:56])
	}
	if bitCount == 16 || bitCount == 32 {
		if redMask == 0 && greenMask == 0 && blueMask == 0 {
			if bitCount == 32 {
				redMask, greenMask, blueMask = 0x00ff0000, 0x0000ff00, 0x000000ff
			} else {
				redMask, greenMask, blueMask = 0x7c00, 0x03e0, 0x001f
			}
		}
	}

	var palette color.Palette
	if bitCount <= 8 {
		colors := int(binary.LittleEndian.Uint32(data[32:36]))
		if colors == 0 {
			colors = 1 << bitCount
		}
		if colors > 1<<bitCount || colors < 1 {
			return nil, errors.New("invalid DIB color table")
		}
		paletteBytes := colors * 4
		if offset+paletteBytes > len(data) {
			return nil, errors.New("DIB color table is truncated")
		}
		palette = make(color.Palette, colors)
		for i := range palette {
			entry := data[offset+i*4 : offset+i*4+4]
			palette[i] = color.RGBA{R: entry[2], G: entry[1], B: entry[0], A: 0xff}
		}
		offset += paletteBytes
	}

	rowBits := int64(width) * int64(bitCount)
	stride64 := ((rowBits + 31) / 32) * 4
	pixelBytes := stride64 * int64(height)
	if stride64 > int64(math.MaxInt) || pixelBytes > int64(len(data)-offset) {
		return nil, errors.New("DIB pixel data is truncated")
	}
	stride := int(stride64)
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	topDown := height32 < 0
	for y := 0; y < height; y++ {
		sourceY := y
		if !topDown {
			sourceY = height - 1 - y
		}
		row := data[offset+sourceY*stride : offset+(sourceY+1)*stride]
		for x := 0; x < width; x++ {
			var c color.NRGBA
			switch bitCount {
			case 1:
				idx := int((row[x/8] >> (7 - uint(x%8))) & 1)
				if idx >= len(palette) {
					return nil, errors.New("DIB pixel references a missing palette color")
				}
				p := palette[idx].(color.RGBA)
				c = color.NRGBA{R: p.R, G: p.G, B: p.B, A: p.A}
			case 4:
				v := row[x/2]
				idx := int(v >> 4)
				if x%2 != 0 {
					idx = int(v & 0x0f)
				}
				if idx >= len(palette) {
					return nil, errors.New("DIB pixel references a missing palette color")
				}
				p := palette[idx].(color.RGBA)
				c = color.NRGBA{R: p.R, G: p.G, B: p.B, A: p.A}
			case 8:
				idx := int(row[x])
				if idx >= len(palette) {
					return nil, errors.New("DIB pixel references a missing palette color")
				}
				p := palette[idx].(color.RGBA)
				c = color.NRGBA{R: p.R, G: p.G, B: p.B, A: p.A}
			case 16:
				v := uint32(binary.LittleEndian.Uint16(row[x*2 : x*2+2]))
				c = color.NRGBA{R: maskByte(v, redMask), G: maskByte(v, greenMask), B: maskByte(v, blueMask), A: 0xff}
			case 24:
				c = color.NRGBA{R: row[x*3+2], G: row[x*3+1], B: row[x*3], A: 0xff}
			case 32:
				v := binary.LittleEndian.Uint32(row[x*4 : x*4+4])
				a := byte(0xff)
				if alphaMask != 0 {
					a = maskByte(v, alphaMask)
				}
				c = color.NRGBA{R: maskByte(v, redMask), G: maskByte(v, greenMask), B: maskByte(v, blueMask), A: a}
			}
			out.SetNRGBA(x, y, c)
		}
	}
	return out, nil
}

func maskByte(value, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := uint(0)
	for (mask>>shift)&1 == 0 {
		shift++
	}
	component := (value & mask) >> shift
	maxValue := mask >> shift
	return uint8((uint64(component)*255 + uint64(maxValue)/2) / uint64(maxValue))
}
