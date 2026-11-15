package bundle

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"os"

	"golang.org/x/image/draw"
)

// Icon file formats (sources: en.wikipedia.org/wiki/ICO_(file_format) and
// en.wikipedia.org/wiki/Apple_Icon_Image_format, read 2026-11-15). Every entry is a whole PNG file.

// LoadIcon reads the bundle's icon.
func (b *Bundle) LoadIcon() (image.Image, error) {
	full, err := b.local(b.Icon)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// scaledPNG returns src scaled to n x n pixels, as a PNG file.
func scaledPNG(src image.Image, n int) ([]byte, error) {
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	var buf bytes.Buffer
	err := png.Encode(&buf, dst)
	return buf.Bytes(), err
}

// WriteICO writes a Windows icon with PNG entries of 16, 32, 48 and 256 pixels. All numbers are little-endian:
// a 6-byte header (0, type 1, count), one 16-byte entry per image (width and height, 0 meaning 256; no palette;
// 1 plane; 32 bits per pixel; data size; data offset), then the PNG files in the same order.
func WriteICO(w io.Writer, src image.Image) error {
	sizes := []int{16, 32, 48, 256}
	var head, data bytes.Buffer
	binary.Write(&head, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for _, n := range sizes {
		p, err := scaledPNG(src, n)
		if err != nil {
			return err
		}
		side := byte(n % 256) // 256 is written as 0
		head.Write([]byte{side, side, 0, 0})
		binary.Write(&head, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&head, binary.LittleEndian, [2]uint32{uint32(len(p)), uint32(offset + data.Len())})
		data.Write(p)
	}
	_, err := w.Write(append(head.Bytes(), data.Bytes()...))
	return err
}

// WriteICNS writes a macOS icon with PNG entries of 128, 256 and 512 pixels (types ic07, ic08, ic09). All numbers
// are big-endian: "icns" and the length of the whole file, then per entry its type and its length including these
// 8 bytes, then the PNG file.
func WriteICNS(w io.Writer, src image.Image) error {
	var body bytes.Buffer
	for _, e := range []struct {
		typ string
		n   int
	}{{"ic07", 128}, {"ic08", 256}, {"ic09", 512}} {
		p, err := scaledPNG(src, e.n)
		if err != nil {
			return err
		}
		body.WriteString(e.typ)
		binary.Write(&body, binary.BigEndian, uint32(8+len(p)))
		body.Write(p)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	_, err := w.Write(out.Bytes())
	return err
}
