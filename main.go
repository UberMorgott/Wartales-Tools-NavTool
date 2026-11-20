// navtool adds walkable roads through Wartales world-map towns by editing assets/worldmap/collides/navmesh.dat.
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

const version = "1.0.0"

const usage = `navtool ` + version + ` - walkable roads through Wartales world-map towns (navmesh.dat)

  navtool holes <navmesh.dat> [x y radius]          list holes (towns, rocks, lakes) with centre and size
  navtool png   <navmesh.dat> <x> <y> <radius> <out.png> [spec.txt]
                                                    picture of an area; with a spec its new cells are green
  navtool apply <vanilla navmesh.dat> <spec.txt> <out navmesh.dat>
                                                    apply every spec line to the vanilla file

spec.txt, one command per line, # starts a comment:
  bridge x1 y1 x2 y2    one walkable cell from the town-border edge nearest (x1 y1) to the edge nearest (x2 y2)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func nums(args []string) ([]float64, error) {
	out := make([]float64, len(args))
	for i, a := range args {
		v, err := strconv.ParseFloat(a, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", a)
		}
		out[i] = v
	}
	return out, nil
}

func run(a []string) error {
	if len(a) == 0 {
		fmt.Print(usage)
		return nil
	}
	switch {
	case a[0] == "holes" && (len(a) == 2 || len(a) == 5):
		m, err := Load(a[1])
		if err != nil {
			return err
		}
		hs := m.Holes()
		if len(a) == 5 {
			v, err := nums(a[2:])
			if err != nil {
				return err
			}
			var near []Hole
			for _, h := range hs {
				if (h.Center[0]-v[0])*(h.Center[0]-v[0])+(h.Center[1]-v[1])*(h.Center[1]-v[1]) <= v[2]*v[2] {
					near = append(near, h)
				}
			}
			hs = near
		}
		sort.Slice(hs, func(i, j int) bool { return hs[i].Area > hs[j].Area })
		fmt.Printf("%d cells, max id %d, %d holes listed (largest first)\n", len(m.Cells), m.MaxID(), len(hs))
		fmt.Println("centre x   centre y      area  edges  size x*y")
		for _, h := range hs {
			fmt.Printf("%8.1f  %8.1f  %8.0f  %5d  %.0f*%.0f\n", h.Center[0], h.Center[1], h.Area, h.N, h.Hi[0]-h.Lo[0], h.Hi[1]-h.Lo[1])
		}
		return nil
	case a[0] == "png" && (len(a) == 6 || len(a) == 7):
		m, err := Load(a[1])
		if err != nil {
			return err
		}
		v, err := nums(a[2:5])
		if err != nil {
			return err
		}
		var added []*Cell
		if len(a) == 7 {
			if added, err = applySpec(m, a[6]); err != nil {
				return err
			}
		}
		return writePNG(m, added, v[0], v[1], v[2], a[5])
	case a[0] == "apply" && len(a) == 4:
		m, err := Load(a[1])
		if err != nil {
			return err
		}
		if _, err := applySpec(m, a[2]); err != nil {
			return err
		}
		if err := os.WriteFile(a[3], m.Bytes(), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s: %d cells\n", a[3], len(m.Cells))
		return nil
	}
	fmt.Print(usage)
	return fmt.Errorf("bad command line")
}

func applySpec(m *Mesh, path string) ([]*Cell, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var added []*Cell
	sc := bufio.NewScanner(f)
	for ln := 1; sc.Scan(); ln++ {
		line, _, _ := strings.Cut(sc.Text(), "#")
		w := strings.Fields(line)
		if len(w) == 0 {
			continue
		}
		if w[0] != "bridge" || len(w) != 5 {
			return nil, fmt.Errorf("%s:%d: expected \"bridge x1 y1 x2 y2\"", path, ln)
		}
		v, err := nums(w[1:])
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %v", path, ln, err)
		}
		c, err := m.Bridge([2]float64{v[0], v[1]}, [2]float64{v[2], v[3]})
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %v", path, ln, err)
		}
		fmt.Printf("line %d: new cell %d joins cells %d and %d\n", ln, c.ID, c.Nbrs[0], c.Nbrs[1])
		added = append(added, c)
	}
	return added, sc.Err()
}
