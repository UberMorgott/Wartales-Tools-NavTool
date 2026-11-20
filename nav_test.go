package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// Round trip on the real file: set NAVMESH to a vanilla navmesh.dat (not shipped: game data).
func TestRoundTrip(t *testing.T) {
	path := os.Getenv("NAVMESH")
	if path == "" {
		t.Skip("NAVMESH not set")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.Bytes(), b) {
		t.Fatal("round trip differs")
	}
}

// A 3x3 grid of unit squares with the centre one missing: a hole to bridge. Cell id = 10 + 3*row + col.
func strip() *Mesh {
	m := &Mesh{Key: make([]byte, 16)}
	idx := map[[2]float64]int32{}
	pt := func(x, y float64) int32 { // points numbered by first appearance, like the file
		p := [2]float64{x, y}
		if i, ok := idx[p]; ok {
			return i
		}
		m.Pts = append(m.Pts, p)
		idx[p] = int32(len(m.Pts) - 1)
		return idx[p]
	}
	for j := 0; j < 3; j++ {
		for i := 0; i < 3; i++ {
			if i == 1 && j == 1 {
				continue
			}
			x, y := float64(i), float64(j)
			m.Cells = append(m.Cells, &Cell{ID: int32(10 + 3*j + i), Group: -1, Poly: []int32{pt(x, y), pt(x+1, y), pt(x+1, y+1), pt(x, y+1)}})
		}
	}
	return m
}
func TestBridge(t *testing.T) {
	m := strip()
	c, err := m.Bridge([2]float64{1, 1.5}, [2]float64{2, 1.5})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != 19 || len(c.Poly) != 4 || c.Nbrs[0] != 13 || c.Nbrs[1] != 15 {
		t.Fatalf("got %+v", c)
	}
	if m.Cells[3].Nbrs[0] != 19 || m.Cells[4].Nbrs[0] != 19 {
		t.Fatal("neighbours not linked back")
	}
	m2, err := Parse(m.Bytes())
	if err != nil || len(m2.Cells) != 9 || !bytes.Equal(m2.Bytes(), m.Bytes()) {
		t.Fatal("edited mesh does not survive a round trip", err)
	}
	if hs := strip().Holes(); len(hs) != 1 || hs[0].Area != 1 || hs[0].Center != [2]float64{1.5, 1.5} {
		t.Fatalf("holes %+v", hs)
	}
	// the hole is gone now: a second bridge must not stack on the first
	if _, err := m.Bridge([2]float64{1, 1.5}, [2]float64{2, 1.5}); err == nil {
		t.Fatal("bridge over a filled hole accepted")
	}
}

func TestBridgeRefuses(t *testing.T) {
	for _, tc := range []struct {
		p1, p2 [2]float64
		want   string
	}{
		{[2]float64{1, 1.5}, [2]float64{20, 1.5}, "point 2"},
		{[2]float64{1, 1.5}, [2]float64{1, 1.4}, "both points"},
		{[2]float64{1, 1.5}, [2]float64{2.5, 0}, "convex"}, // the hole's west edge and the map's bottom border
	} {
		_, err := strip().Bridge(tc.p1, tc.p2)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v %v: err %v, want %q", tc.p1, tc.p2, err, tc.want)
		}
	}
}
