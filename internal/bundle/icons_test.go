package bundle

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func testIcon() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	for y := range 300 {
		for x := range 300 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	return img
}

// pngSize decodes a PNG entry and returns its width and height.
func pngSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("entry is no PNG: %v", err)
	}
	return img.Bounds().Dx(), img.Bounds().Dy()
}

func TestWriteICO(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteICO(&buf, testIcon()); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	le := binary.LittleEndian
	if le.Uint16(b[0:]) != 0 || le.Uint16(b[2:]) != 1 || le.Uint16(b[4:]) != 4 {
		t.Fatalf("header % x", b[:6])
	}
	for i, want := range []int{16, 32, 48, 256} {
		e := b[6+16*i:]
		size, offset := le.Uint32(e[8:]), le.Uint32(e[12:])
		if int(offset)+int(size) > len(b) || e[2] != 0 || e[3] != 0 || le.Uint16(e[4:]) != 1 || le.Uint16(e[6:]) != 32 {
			t.Fatalf("entry %d: % x", i, e[:16])
		}
		side := int(e[0])
		if side == 0 {
			side = 256
		}
		if w, h := pngSize(t, b[offset:offset+size]); side != want || int(e[1]) != want%256 || w != want || h != want {
			t.Fatalf("entry %d: header %d, PNG %dx%d, want %d", i, side, w, h, want)
		}
	}
}

func TestWriteICNS(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteICNS(&buf, testIcon()); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	be := binary.BigEndian
	if string(b[:4]) != "icns" || int(be.Uint32(b[4:])) != len(b) {
		t.Fatalf("header % x, file %d bytes", b[:8], len(b))
	}
	var got []string
	for pos := 8; pos < len(b); {
		typ, n := string(b[pos:pos+4]), int(be.Uint32(b[pos+4:]))
		if n < 8 || pos+n > len(b) {
			t.Fatalf("entry %s has length %d", typ, n)
		}
		w, h := pngSize(t, b[pos+8:pos+n])
		want := map[string]int{"ic07": 128, "ic08": 256, "ic09": 512}[typ]
		if w != want || h != want {
			t.Fatalf("%s: %dx%d, want %d", typ, w, h, want)
		}
		got = append(got, typ)
		pos += n
	}
	if len(got) != 3 {
		t.Fatalf("entries %v", got)
	}
}
