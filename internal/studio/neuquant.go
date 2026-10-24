package studio

import "math"

// neuQuant is Anthony Dekker's NeuQuant colour quantizer as the color_quant crate (1.1.0)
// implements it, which the Rust exporter trains its GIF palette with. The arithmetic and the
// sampling order are kept so both builds pick the same palette.
type neuQuant struct {
	network   []neuron
	colormap  []quadI
	netindex  [256]int
	bias      []float64
	freq      []float64
	samplefac int
	netsize   int
}

type neuron struct{ r, g, b, a float64 }
type quadI struct{ r, g, b, a int32 }

const (
	nqChannels       = 4
	nqRadiusDec      = 30
	nqAlphaBiasShift = 10
	nqInitAlpha      = 1 << nqAlphaBiasShift
	nqGamma          = 1024.0
	nqBeta           = 1.0 / nqGamma
	nqBetaGamma      = nqBeta * nqGamma
)

var nqPrimes = [4]int{499, 491, 478, 503}

// newNeuQuant trains a palette of colors entries on RGBA pixels; samplefac 1..30 trades
// quality for speed.
func newNeuQuant(samplefac, colors int, pixels []byte) *neuQuant {
	nq := &neuQuant{samplefac: samplefac, netsize: colors}
	freq := 1 / float64(colors)
	for i := range colors {
		tmp := float64(i) * 256 / float64(colors)
		a := 255.0
		if i < 16 {
			a = float64(i) * 16
		}
		nq.network = append(nq.network, neuron{tmp, tmp, tmp, a})
		nq.colormap = append(nq.colormap, quadI{0, 0, 0, 255})
		nq.freq = append(nq.freq, freq)
		nq.bias = append(nq.bias, 0)
	}
	nq.learn(pixels)
	nq.buildColormap()
	nq.buildNetindex()
	return nq
}

func (nq *neuQuant) indexOf(r, g, b, a uint8) int { return nq.searchNetindex(b, g, r, a) }

func (nq *neuQuant) colorMapRGB() []byte {
	out := make([]byte, 0, nq.netsize*3)
	for _, c := range nq.colormap {
		out = append(out, uint8(c.r), uint8(c.g), uint8(c.b))
	}
	return out
}

func (nq *neuQuant) salterSingle(alpha float64, i int, q neuron) {
	n := &nq.network[i]
	n.b -= alpha * (n.b - q.b)
	n.g -= alpha * (n.g - q.g)
	n.r -= alpha * (n.r - q.r)
	n.a -= alpha * (n.a - q.a)
}

func (nq *neuQuant) alterNeighbour(alpha float64, rad, i int, q neuron) {
	lo := max(i-rad, 0)
	hi := min(i+rad, nq.netsize)
	j, k, m := i+1, i-1, 0
	for j < hi || k > lo {
		radSq := float64(rad) * float64(rad)
		a := (alpha * (radSq - float64(m)*float64(m))) / radSq
		m++
		if j < hi {
			p := &nq.network[j]
			p.b -= a * (p.b - q.b)
			p.g -= a * (p.g - q.g)
			p.r -= a * (p.r - q.r)
			p.a -= a * (p.a - q.a)
			j++
		}
		if k > lo {
			p := &nq.network[k]
			p.b -= a * (p.b - q.b)
			p.g -= a * (p.g - q.g)
			p.r -= a * (p.r - q.r)
			p.a -= a * (p.a - q.a)
			k--
		}
	}
}

// contest finds the closest neuron, updates the frequencies, and returns the best neuron
// once frequently chosen ones are biased against.
func (nq *neuQuant) contest(b, g, r, a float64) int {
	bestd := math.MaxFloat64
	bestbiasd := bestd
	bestpos, bestbiaspos := -1, -1
	for i := range nq.netsize {
		bestbiasdBiased := bestbiasd + nq.bias[i]
		n := &nq.network[i]
		dist := math.Abs(n.b - b)
		dist += math.Abs(n.r - r)
		if dist < bestd || dist < bestbiasdBiased {
			dist += math.Abs(n.g - g)
			dist += math.Abs(n.a - a)
			if dist < bestd {
				bestd, bestpos = dist, i
			}
			if biasdist := dist - nq.bias[i]; biasdist < bestbiasd {
				bestbiasd, bestbiaspos = biasdist, i
			}
		}
		nq.freq[i] -= nqBeta * nq.freq[i]
		nq.bias[i] += nqBetaGamma * nq.freq[i]
	}
	nq.freq[bestpos] += nqBeta
	nq.bias[bestpos] -= nqBetaGamma
	return bestbiaspos
}

func (nq *neuQuant) learn(pixels []byte) {
	initrad := nq.netsize / 8
	const radiusBiasShift = 6
	const radiusBias = 1 << radiusBiasShift
	biasRadius := initrad * radiusBias
	alphadec := 30 + (nq.samplefac-1)/3
	lengthcount := len(pixels) / nqChannels
	samplepixels := lengthcount / nq.samplefac
	nCycles := nq.netsize >> 1
	if nCycles <= 100 {
		nCycles = 100
	}
	delta := samplepixels / nCycles
	if delta == 0 {
		delta = 1
	}
	alpha := nqInitAlpha
	rad := biasRadius >> radiusBiasShift
	if rad <= 1 {
		rad = 0
	}
	step := nqPrimes[3]
	for _, p := range nqPrimes {
		if lengthcount%p != 0 {
			step = p
			break
		}
	}
	pos := 0
	for i := 0; i < samplepixels; {
		p := pixels[nqChannels*pos : nqChannels*pos+nqChannels]
		q := neuron{r: float64(p[0]), g: float64(p[1]), b: float64(p[2]), a: float64(p[3])}
		j := nq.contest(q.b, q.g, q.r, q.a)
		a := float64(alpha) / nqInitAlpha
		nq.salterSingle(a, j, q)
		if rad > 0 {
			nq.alterNeighbour(a, rad, j, q)
		}
		pos += step
		for pos >= lengthcount {
			pos -= lengthcount
		}
		i++
		if i%delta == 0 {
			alpha -= alpha / alphadec
			biasRadius -= biasRadius / nqRadiusDec
			rad = biasRadius >> radiusBiasShift
			if rad <= 1 {
				rad = 0
			}
		}
	}
}

func (nq *neuQuant) buildColormap() {
	c := func(v float64) int32 { return int32(min(max(math.Round(v), 0), 255)) }
	for i, n := range nq.network {
		nq.colormap[i] = quadI{r: c(n.r), g: c(n.g), b: c(n.b), a: c(n.a)}
	}
}

// buildNetindex sorts the colormap by green and indexes where each green value starts.
func (nq *neuQuant) buildNetindex() {
	previouscol, startpos := 0, 0
	for i := range nq.netsize {
		smallpos := i
		smallval := int(nq.colormap[i].g)
		for j := i + 1; j < nq.netsize; j++ {
			if g := int(nq.colormap[j].g); g < smallval {
				smallpos, smallval = j, g
			}
		}
		if i != smallpos {
			nq.colormap[i], nq.colormap[smallpos] = nq.colormap[smallpos], nq.colormap[i]
		}
		if smallval != previouscol {
			nq.netindex[previouscol] = (startpos + i) >> 1
			for j := previouscol + 1; j < smallval; j++ {
				nq.netindex[j] = i
			}
			previouscol, startpos = smallval, i
		}
	}
	maxNetpos := nq.netsize - 1
	nq.netindex[previouscol] = (startpos + maxNetpos) >> 1
	for j := previouscol + 1; j < 256; j++ {
		nq.netindex[j] = maxNetpos
	}
}

func (nq *neuQuant) searchNetindex(b, g, r, a uint8) int {
	bestd := int32(1 << 30)
	best := 0
	i := nq.netindex[g]
	j := 0
	if i > 0 {
		j = i - 1
	}
	// dist is the squared distance to entry k, or false once even its green alone is too far.
	consider := func(k int) bool {
		p := nq.colormap[k]
		e := p.g - int32(g)
		dist := e * e
		if dist >= bestd {
			return false
		}
		e = p.b - int32(b)
		dist += e * e
		if dist < bestd {
			e = p.r - int32(r)
			dist += e * e
			if dist < bestd {
				e = p.a - int32(a)
				dist += e * e
				if dist < bestd {
					bestd, best = dist, k
				}
			}
		}
		return true
	}
	for i < nq.netsize || j > 0 {
		if i < nq.netsize {
			if !consider(i) {
				break
			}
			i++
		}
		if j > 0 {
			if !consider(j) {
				break
			}
			j--
		}
	}
	return best
}
