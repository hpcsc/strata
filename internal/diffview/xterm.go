package diffview

import (
	"math"
	"slices"
)

const (
	maxHueShift = 35.0
	minChroma   = 0.03
)

type xtermColor struct {
	rgb rgb
	lab oklab
}

var xtermCube = func() []xtermColor {
	levels := [6]uint8{0, 95, 135, 175, 215, 255}
	out := make([]xtermColor, 0, 216)
	for i := range 216 {
		c := rgb{levels[i/36], levels[i/6%6], levels[i%6], true}
		out = append(out, xtermColor{rgb: c, lab: c.lab()})
	}
	return out
}()

var xtermGreys = func() []xtermColor {
	var out []xtermColor
	for _, v := range []uint8{0, 95, 135, 175, 215, 255} {
		c := rgb{v, v, v, true}
		out = append(out, xtermColor{rgb: c, lab: c.lab()})
	}
	for i := range 24 {
		v := uint8(8 + 10*i)
		c := rgb{v, v, v, true}
		out = append(out, xtermColor{rgb: c, lab: c.lab()})
	}
	return out
}()

func greyIn256(c rgb) rgb {
	want := c.lab().l
	best := xtermGreys[0]
	for _, g := range xtermGreys[1:] {
		if math.Abs(g.lab.l-want) < math.Abs(best.lab.l-want) {
			best = g
		}
	}
	return best.rgb
}

func sameHueIn256(c rgb, avoid ...rgb) rgb {
	want := c.lab()
	best, bestDistance := c, math.Inf(1)
	for _, x := range xtermCube {
		if x.lab.chroma() < minChroma || hueShift(x.lab, want) > maxHueShift || slices.Contains(avoid, x.rgb) {
			continue
		}
		if d := x.lab.distance(want); d < bestDistance {
			best, bestDistance = x.rgb, d
		}
	}
	return best
}

func hueShift(x, y oklab) float64 {
	d := math.Abs(math.Atan2(x.b, x.a)-math.Atan2(y.b, y.a)) * 180 / math.Pi
	return math.Min(d, 360-d)
}
