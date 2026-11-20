package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// Mesh is assets/worldmap/collides/navmesh.dat (shiro.navmesh.Serializer):
// 16 B key, i32 cell count, per cell i32 id, group, collideBits, nPts and its points
// (f64 x,y, or a NaN slot holding the i32 index of an earlier point), then per cell i32 nNbr + neighbour ids.
type Mesh struct {
	Key   []byte
	Pts   [][2]float64
	Cells []*Cell
}

type Cell struct {
	ID, Group, Bits int32
	Poly            []int32 // indices into Pts, counter-clockwise (positive area)
	Nbrs            []int32 // neighbour cell ids
}

const nanHigh = 0x7fffffff // high word of the back-reference slot: makes the f64 a NaN

func Load(path string) (*Mesh, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return m, nil
}

func Parse(b []byte) (m *Mesh, err error) {
	defer func() {
		if recover() != nil {
			m, err = nil, fmt.Errorf("not a navmesh.dat (truncated)")
		}
	}()
	p := 16
	i32 := func() int32 { v := int32(binary.LittleEndian.Uint32(b[p:])); p += 4; return v }
	f64 := func() float64 { v := math.Float64frombits(binary.LittleEndian.Uint64(b[p:])); p += 8; return v }
	m = &Mesh{Key: b[:16:16]}
	n := i32()
	if n < 0 || int(n) > len(b)/16 {
		return nil, fmt.Errorf("not a navmesh.dat (cell count %d)", n)
	}
	for ; n > 0; n-- {
		c := &Cell{ID: i32(), Group: i32(), Bits: i32()}
		k := i32()
		for ; k > 0; k-- {
			if binary.LittleEndian.Uint32(b[p+4:]) == nanHigh {
				idx := i32()
				p += 4
				if idx < 0 || int(idx) >= len(m.Pts) {
					return nil, fmt.Errorf("bad point reference %d in cell %d", idx, c.ID)
				}
				c.Poly = append(c.Poly, idx)
			} else {
				m.Pts = append(m.Pts, [2]float64{f64(), f64()})
				c.Poly = append(c.Poly, int32(len(m.Pts)-1))
			}
		}
		m.Cells = append(m.Cells, c)
	}
	for _, c := range m.Cells {
		k := i32()
		for ; k > 0; k-- {
			c.Nbrs = append(c.Nbrs, i32())
		}
	}
	if p != len(b) {
		return nil, fmt.Errorf("%d trailing bytes", len(b)-p)
	}
	return m, nil
}

func (m *Mesh) Bytes() []byte {
	out := append([]byte{}, m.Key...)
	put := func(v int32) { out = binary.LittleEndian.AppendUint32(out, uint32(v)) }
	put(int32(len(m.Cells)))
	next := int32(0) // points are numbered by first appearance, so a point is new exactly when it is the next index
	for _, c := range m.Cells {
		put(c.ID)
		put(c.Group)
		put(c.Bits)
		put(int32(len(c.Poly)))
		for _, i := range c.Poly {
			if i == next {
				out = binary.LittleEndian.AppendUint64(out, math.Float64bits(m.Pts[i][0]))
				out = binary.LittleEndian.AppendUint64(out, math.Float64bits(m.Pts[i][1]))
				next++
			} else {
				put(i)
				put(nanHigh)
			}
		}
	}
	for _, c := range m.Cells {
		put(int32(len(c.Nbrs)))
		for _, n := range c.Nbrs {
			put(n)
		}
	}
	return out
}

func (m *Mesh) MaxID() int32 {
	var mx int32
	for _, c := range m.Cells {
		mx = max(mx, c.ID)
	}
	return mx
}

// edge is an undirected edge between two point indices (after merging identical coordinates).
type edge [2]int32

type side struct {
	cell *Cell
	a, b int32 // the edge in the cell's own (counter-clockwise) order, original point indices
}

// edges maps every edge to the cells using it; an edge used once lies on a hole or the map border.
func (m *Mesh) edges() map[edge][]side {
	canon := map[[2]float64]int32{}
	id := make([]int32, len(m.Pts))
	for i, p := range m.Pts {
		if j, ok := canon[p]; ok {
			id[i] = j
		} else {
			canon[p] = int32(i)
			id[i] = int32(i)
		}
	}
	es := map[edge][]side{}
	for _, c := range m.Cells {
		for k, a := range c.Poly {
			b := c.Poly[(k+1)%len(c.Poly)]
			e := edge{min(id[a], id[b]), max(id[a], id[b])}
			es[e] = append(es[e], side{c, a, b})
		}
	}
	return es
}

func (m *Mesh) boundary() []side {
	var out []side
	for _, s := range m.edges() {
		if len(s) == 1 {
			out = append(out, s[0])
		}
	}
	return out
}

func segDist(p, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	t := ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p[0]-a[0]-t*dx, p[1]-a[1]-t*dy)
}

func (m *Mesh) nearestBoundary(bd []side, p [2]float64) (side, float64) {
	best, bd2 := side{}, math.Inf(1)
	for _, s := range bd {
		if d := segDist(p, m.Pts[s.a], m.Pts[s.b]); d < bd2 {
			best, bd2 = s, d
		}
	}
	return best, bd2
}

func cross(o, a, b [2]float64) float64 {
	return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0])
}

// MaxSnap is how far (world units) a spec point may be from the hole edge it picks.
const MaxSnap = 10.0

// Bridge adds one convex cell joining the hole edges nearest p1 and p2 and links it to both cells on the other side.
func (m *Mesh) Bridge(p1, p2 [2]float64) (*Cell, error) {
	bd := m.boundary()
	s1, d1 := m.nearestBoundary(bd, p1)
	s2, d2 := m.nearestBoundary(bd, p2)
	switch {
	case d1 > MaxSnap:
		return nil, fmt.Errorf("point 1 is %.1f from the nearest hole edge (max %.0f): put it on the town's border", d1, MaxSnap)
	case d2 > MaxSnap:
		return nil, fmt.Errorf("point 2 is %.1f from the nearest hole edge (max %.0f): put it on the town's border", d2, MaxSnap)
	case s1.cell == s2.cell:
		return nil, fmt.Errorf("both points snap to cell %d: put them on opposite sides of the town", s1.cell.ID)
	}
	// Walk both edges backwards: the new cell lies on their other side.
	var poly []int32
	for _, i := range []int32{s1.a, s2.b, s2.a, s1.b} {
		if len(poly) == 0 || m.Pts[poly[len(poly)-1]] != m.Pts[i] {
			poly = append(poly, i)
		}
	}
	if m.Pts[poly[0]] == m.Pts[poly[len(poly)-1]] {
		poly = poly[:len(poly)-1]
	}
	if len(poly) < 3 {
		return nil, fmt.Errorf("both points snap to the same edge")
	}
	n := len(poly)
	for k := range poly {
		if cross(m.Pts[poly[k]], m.Pts[poly[(k+1)%n]], m.Pts[poly[(k+2)%n]]) <= 1e-9 {
			return nil, fmt.Errorf("the new cell would not be convex (edges at %s and %s): pick edges facing each other", fmtP(m.Pts[s1.a]), fmtP(m.Pts[s2.a]))
		}
	}
	c := &Cell{ID: m.MaxID() + 1, Group: s1.cell.Group, Bits: s1.cell.Bits, Poly: poly, Nbrs: []int32{s1.cell.ID, s2.cell.ID}}
	if err := m.free(c); err != nil {
		return nil, err
	}
	m.Cells = append(m.Cells, c)
	s1.cell.Nbrs = append(s1.cell.Nbrs, c.ID)
	s2.cell.Nbrs = append(s2.cell.Nbrs, c.ID)
	return c, nil
}

func fmtP(p [2]float64) string { return fmt.Sprintf("(%.1f %.1f)", p[0], p[1]) }

func bounds(m *Mesh, poly []int32) (lo, hi [2]float64) {
	lo, hi = [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
	for _, i := range poly {
		for k := 0; k < 2; k++ {
			lo[k] = math.Min(lo[k], m.Pts[i][k])
			hi[k] = math.Max(hi[k], m.Pts[i][k])
		}
	}
	return
}

// inside reports whether p is strictly inside the convex counter-clockwise polygon.
func inside(m *Mesh, poly []int32, p [2]float64) bool {
	for k, a := range poly {
		if cross(m.Pts[a], m.Pts[poly[(k+1)%len(poly)]], p) <= 1e-9 {
			return false
		}
	}
	return true
}

// free refuses a new cell that overlaps any existing cell (walkable ground or a crossing edge), i.e. one outside the hole.
func (m *Mesh) free(c *Cell) error {
	lo, hi := bounds(m, c.Poly)
	var mid [2]float64
	for _, i := range c.Poly {
		mid[0] += m.Pts[i][0] / float64(len(c.Poly))
		mid[1] += m.Pts[i][1] / float64(len(c.Poly))
	}
	for _, o := range m.Cells {
		olo, ohi := bounds(m, o.Poly)
		if olo[0] > hi[0] || olo[1] > hi[1] || ohi[0] < lo[0] || ohi[1] < lo[1] {
			continue
		}
		if inside(m, o.Poly, mid) {
			return fmt.Errorf("the new cell lies on walkable cell %d, not inside a hole", o.ID)
		}
		for k, a := range o.Poly {
			b := o.Poly[(k+1)%len(o.Poly)]
			if inside(m, c.Poly, m.Pts[a]) {
				return fmt.Errorf("the new cell covers a point %s of cell %d: the hole is not convex here, pick other points", fmtP(m.Pts[a]), o.ID)
			}
			for j, x := range c.Poly {
				y := c.Poly[(j+1)%len(c.Poly)]
				if crosses(m.Pts[a], m.Pts[b], m.Pts[x], m.Pts[y]) {
					return fmt.Errorf("the new cell crosses an edge of cell %d at %s: pick other points", o.ID, fmtP(m.Pts[a]))
				}
			}
		}
	}
	return nil
}

// crosses reports a proper intersection (touching at an end point does not count).
func crosses(a, b, c, d [2]float64) bool {
	const e = 1e-9
	d1, d2 := cross(a, b, c), cross(a, b, d)
	d3, d4 := cross(c, d, a), cross(c, d, b)
	return ((d1 > e && d2 < -e) || (d1 < -e && d2 > e)) && ((d3 > e && d4 < -e) || (d3 < -e && d4 > e))
}

// Hole is one closed loop of boundary edges with the walkable side outside.
type Hole struct {
	Center [2]float64
	Area   float64
	N      int
	Lo, Hi [2]float64
}

// Holes returns every loop that encloses unwalkable ground (clockwise loops; the map border is counter-clockwise).
func (m *Mesh) Holes() []Hole {
	bd := m.boundary()
	next := map[[2]float64][]int{}
	for i, s := range bd {
		next[m.Pts[s.a]] = append(next[m.Pts[s.a]], i)
	}
	used := make([]bool, len(bd))
	var out []Hole
	for start := range bd {
		if used[start] {
			continue
		}
		var loop [][2]float64
		for i := start; i >= 0; {
			used[i] = true
			loop = append(loop, m.Pts[bd[i].a])
			j := -1
			for _, k := range next[m.Pts[bd[i].b]] {
				if !used[k] {
					j = k
					break
				}
			}
			i = j
		}
		h := Hole{N: len(loop), Lo: loop[0], Hi: loop[0]}
		var cx, cy float64
		for k, p := range loop {
			q := loop[(k+1)%len(loop)]
			a := p[0]*q[1] - q[0]*p[1]
			h.Area += a / 2
			cx += (p[0] + q[0]) * a / 6
			cy += (p[1] + q[1]) * a / 6
			h.Lo = [2]float64{math.Min(h.Lo[0], p[0]), math.Min(h.Lo[1], p[1])}
			h.Hi = [2]float64{math.Max(h.Hi[0], p[0]), math.Max(h.Hi[1], p[1])}
		}
		if h.Area < 0 {
			h.Area = -h.Area
			h.Center = [2]float64{-cx / h.Area, -cy / h.Area}
			out = append(out, h)
		}
	}
	return out
}
