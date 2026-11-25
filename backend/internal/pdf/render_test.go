package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A nonzero crop origin and four rotations exercise the same visible-page coordinates as PDF.js.
func rotatedSource() []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 4 /Kids [3 0 R 4 0 R 5 0 R 6 0 R] >>"}
	for _, rotation := range []int{0, 90, 180, 270} {
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /Resources << >> /MediaBox [0 0 400 450] /CropBox [50 90 290 250] /Rotate %d /Contents 7 0 R >>", rotation))
	}
	stream := "0 0 1 rg 162 165 15 12 re f\n"
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream))
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	var offsets []int
	for i, o := range objects {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, n := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", n)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, start)
	return buf.Bytes()
}
func TestFlattenedVisiblePages(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "original.pdf"), rotatedSource(), 0600); e != nil {
		t.Fatal(e)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	if e := os.WriteFile(filepath.Join(dir, "mark.png"), b.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	name := "Asha Rao"
	var fields []Mark
	for i := 1; i <= 4; i++ {
		fields = append(fields, Mark{Type: "signature", Page: i, X: .65, Y: .1, Width: .2, Height: .2, Image: "mark.png"}, Mark{Type: "name", Page: i, X: .2, Y: .6, Width: .6, Height: .2, Text: &name})
		for j, checked := range []bool{false, true} {
			fields = append(fields, Mark{Type: "checkbox", Page: i, X: .05 + .25*float64(j), Y: .05, Width: .15, Height: .15, Checked: &checked})
		}
	}
	if e := Render(t.Context(), dir, 4, fields); e != nil {
		t.Fatal(e)
	}
	for _, file := range []string{"original", "result"} {
		cmd := exec.Command("pdftoppm", "-cropbox", "-r", "72", "-png", file+".pdf", file)
		cmd.Dir = dir
		if e := cmd.Run(); e != nil {
			t.Fatal(e)
		}
	}
	for i := 1; i <= 4; i++ {
		read := func(base string) image.Image {
			f, e := os.Open(filepath.Join(dir, fmt.Sprintf("%s-%d.png", base, i)))
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			im, e := png.Decode(f)
			if e != nil {
				t.Fatal(e)
			}
			return im
		}
		src, out := read("original"), read("result")
		if src.Bounds() != out.Bounds() {
			t.Fatalf("page %d dimensions changed", i)
		}
		w, h := src.Bounds().Dx(), src.Bounds().Dy()
		red, g, blue, _ := out.At(w*3/4, h/5).RGBA()
		if red < 60000 || g > 2000 || blue > 2000 {
			t.Fatalf("image misplaced on page %d", i)
		}
		// The two checkbox outlines match; only the checked box has ink inside.
		size := min(float64(w)*.15, float64(h)*.15)
		for j := range 2 {
			cx, cy := float64(w)*(.125+.25*float64(j)), float64(h)*.125
			dark := 0
			for y := int(cy - size*.30); y < int(cy+size*.30); y++ {
				for x := int(cx - size*.30); x < int(cx+size*.30); x++ {
					r, _, _, _ := out.At(x, y).RGBA()
					if r < 20000 {
						dark++
					}
				}
			}
			if (dark > 0) != (j == 1) {
				t.Fatalf("incorrect checkbox %d on page %d", j, i)
			}
		}
		// Confirm that the original blue landmark survived at the identical screen position.
		matches := 0
		for y := 2; y < h-2; y++ {
			for x := 2; x < w-2; x++ {
				r, _, b, _ := src.At(x, y).RGBA()
				if b > 60000 && r < 1000 {
					r, _, b, _ = out.At(x, y).RGBA()
					if b > 60000 && r < 1000 {
						matches++
					}
				}
			}
		}
		if matches < 100 {
			t.Fatalf("original shifted on page %d", i)
		}
	}
	text, e := exec.Command("pdftotext", filepath.Join(dir, "result.pdf"), "-").Output()
	if e != nil || strings.Count(string(text), name) != 4 {
		t.Fatal("accepted names missing from PDF", e)
	}
}
