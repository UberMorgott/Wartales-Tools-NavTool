package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const imgSize = 1024

// 3x5 digit glyphs, one row per 3 bits, for the grid labels.
var glyphs = map[rune][5]uint8{
	'0': {7, 5, 5, 5, 7}, '1': {2, 6, 2, 2, 7}, '2': {7, 1, 7, 4, 7}, '3': {7, 1, 7, 1, 7}, '4': {5, 5, 7, 1, 1},
	'5': {7, 4, 7, 1, 7}, '6': {7, 4, 7, 5, 7}, '7': {7, 1, 1, 1, 1}, '8': {7, 5, 7, 5, 7}, '9': {7, 5, 7, 1, 7},
	'-': {0, 0, 7, 0, 0},
}

func text(img *image.RGBA, x, y int, s string, c color.RGBA) {
	const sc = 3
	for _, r := range s {
		g := glyphs[r]
		for row := 0; row < 5; row++ {
			for col := 0; col < 3; col++ {
				if g[row]>>(2-col)&1 == 1 {
					for dy := 0; dy < sc; dy++ {
						for dx := 0; dx < sc; dx++ {
							img.SetRGBA(x+col*sc+dx, y+row*sc+dy, c)
						}
					}
				}
			}
		}
		x += 4 * sc
	}
}

// writePNG draws the square [cx-r, cx+r] x [cy-r, cy+r] with +y down as in the world map: walkable cells light, holes dark,
// added cells green, and a labelled coordinate grid (vertical lines are x, horizontal lines are y).
func writePNG(m *Mesh, added []*Cell, cx, cy, r float64, path string) error {
	img := image.NewRGBA(image.Rect(0, 0, imgSize, imgSize))
	s := imgSize / (2 * r)
	toPx := func(p [2]float64) (float64, float64) { return (p[0] - cx + r) * s, (p[1] - cy + r) * s }
	toW := func(px, py int) [2]float64 {
		return [2]float64{cx - r + (float64(px)+.5)/s, cy - r + (float64(py)+.5)/s}
	}
	for i := range img.Pix {
		img.Pix[i] = 0x30
		if i%4 == 3 {
			img.Pix[i] = 0xff
		}
	}
	isNew := map[*Cell]bool{}
	for _, c := range added {
		isNew[c] = true
	}
	walk, green, line := color.RGBA{0xd8, 0xd0, 0xb8, 0xff}, color.RGBA{0x3c, 0xb0, 0x43, 0xff}, color.RGBA{0x90, 0x88, 0x70, 0xff}
	for _, c := range m.Cells {
		lo, hi := bounds(m, c.Poly)
		x0, y0 := toPx(lo)
		x1, y1 := toPx(hi)
		if x1 < 0 || y1 < 0 || x0 >= imgSize || y0 >= imgSize {
			continue
		}
		col := walk
		if isNew[c] {
			col = green
		}
		for py := max(0, int(y0)); py <= min(imgSize-1, int(y1)); py++ {
			for px := max(0, int(x0)); px <= min(imgSize-1, int(x1)); px++ {
				if insideOrOn(m, c.Poly, toW(px, py)) {
					img.SetRGBA(px, py, col)
				}
			}
		}
		for k, a := range c.Poly {
			ax, ay := toPx(m.Pts[a])
			bx, by := toPx(m.Pts[c.Poly[(k+1)%len(c.Poly)]])
			n := int(math.Max(math.Abs(bx-ax), math.Abs(by-ay))) + 1
			for t := 0; t <= n; t++ {
				x, y := int(ax+(bx-ax)*float64(t)/float64(n)), int(ay+(by-ay)*float64(t)/float64(n))
				if image.Pt(x, y).In(img.Rect) {
					img.SetRGBA(x, y, line)
				}
			}
		}
	}
	// grid step: a round number giving 4..10 lines across
	step := math.Pow(10, math.Floor(math.Log10(2*r)))
	for 2*r/step < 4 {
		step /= 2
	}
	blue := color.RGBA{0x20, 0x60, 0xff, 0xff}
	for g := math.Ceil((cx-r)/step) * step; g <= cx+r; g += step {
		px, _ := toPx([2]float64{g, 0})
		for py := 0; py < imgSize; py++ {
			img.SetRGBA(int(px), py, blue)
		}
		text(img, int(px)+3, 3, fmt.Sprint(math.Round(g)), blue)
	}
	for g := math.Ceil((cy-r)/step) * step; g <= cy+r; g += step {
		_, py := toPx([2]float64{0, g})
		for px := 0; px < imgSize; px++ {
			img.SetRGBA(px, int(py), blue)
		}
		text(img, 3, int(py)+3, fmt.Sprint(math.Round(g)), blue)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	fmt.Printf("wrote %s: x %.1f..%.1f left to right, y %.1f..%.1f top to bottom, grid every %g\n", path, cx-r, cx+r, cy-r, cy+r, step)
	return f.Close()
}

func insideOrOn(m *Mesh, poly []int32, p [2]float64) bool {
	for k, a := range poly {
		if cross(m.Pts[a], m.Pts[poly[(k+1)%len(poly)]], p) < 0 {
			return false
		}
	}
	return true
}
