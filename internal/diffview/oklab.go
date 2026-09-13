package diffview

import "math"

type oklab struct {
	l, a, b float64
}

type tint struct {
	lightness float64
	chroma    float64
}

func (t tint) of(accent, base rgb) rgb {
	a, b := accent.lab(), base.lab()
	return oklab{l: b.l + (a.l-b.l)*t.lightness, a: a.a * t.chroma, b: a.b * t.chroma}.rgb()
}

func blend(c, base rgb, share float64) rgb {
	x, y := c.lab(), base.lab()
	return oklab{l: y.l + (x.l-y.l)*share, a: y.a + (x.a-y.a)*share, b: y.b + (x.b-y.b)*share}.rgb()
}

func (c rgb) dark() bool {
	return c.lab().l < 0.5
}

func (c rgb) lab() oklab {
	r, g, b := linear(c.r), linear(c.g), linear(c.b)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return oklab{
		l: 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		a: 1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		b: 0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func (c oklab) rgb() rgb {
	l := c.l + 0.3963377774*c.a + 0.2158037573*c.b
	m := c.l - 0.1055613458*c.a - 0.0638541728*c.b
	s := c.l - 0.0894841775*c.a - 1.2914855480*c.b
	l, m, s = l*l*l, m*m*m, s*s*s
	return rgb{
		r:   encode(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		g:   encode(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		b:   encode(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s),
		set: true,
	}
}

func linear(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func encode(c float64) uint8 {
	if c <= 0.0031308 {
		c *= 12.92
	} else {
		c = 1.055*math.Pow(c, 1/2.4) - 0.055
	}
	return uint8(math.Round(255 * min(max(c, 0), 1)))
}
