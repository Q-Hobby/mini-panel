// Package qr draws the QR code of the two-step sign-in: the address an authenticator app reads. It knows only what that needs: byte
// mode, error correction level M, versions 1 to 10 (up to 213 bytes; such an address has about 120). It follows ISO/IEC 18004 and
// replaces a library that is no longer kept up.
package qr

import "errors"

// version holds what differs from one size to the next at level M.
type version struct {
	ec     int      // error correction codewords per block
	groups [][2]int // {blocks, data codewords per block}
	align  []int    // centres of the alignment patterns, on both axes
}

var versions = [...]version{
	1:  {10, [][2]int{{1, 16}}, nil},
	2:  {16, [][2]int{{1, 28}}, []int{6, 18}},
	3:  {26, [][2]int{{1, 44}}, []int{6, 22}},
	4:  {18, [][2]int{{2, 32}}, []int{6, 26}},
	5:  {24, [][2]int{{2, 43}}, []int{6, 30}},
	6:  {16, [][2]int{{4, 27}}, []int{6, 34}},
	7:  {18, [][2]int{{4, 31}}, []int{6, 22, 38}},
	8:  {22, [][2]int{{2, 38}, {2, 39}}, []int{6, 24, 42}},
	9:  {22, [][2]int{{3, 36}, {2, 37}}, []int{6, 26, 46}},
	10: {26, [][2]int{{4, 43}, {1, 44}}, []int{6, 28, 50}},
}

func (v version) dataCodewords() int {
	n := 0
	for _, g := range v.groups {
		n += g[0] * g[1]
	}
	return n
}

func countBits(v int) int {
	if v >= 10 {
		return 16
	}
	return 8
}

// capacity is the number of bytes version v holds.
func capacity(v int) int { return (versions[v].dataCodewords()*8 - 4 - countBits(v)) / 8 }

// MaxBytes is the longest text Encode takes.
var MaxBytes = capacity(len(versions) - 1)

// Encode returns the modules of the smallest symbol that holds text, true for dark, row by row, without the quiet zone around it.
func Encode(text string) ([][]bool, error) { return encode(text, -1) }

// encode is Encode with the mask fixed (0 to 7), or chosen by the rules of the standard when mask is -1.
func encode(text string, mask int) ([][]bool, error) {
	data := []byte(text)
	v := 0
	for i := 1; i < len(versions); i++ {
		if len(data) <= capacity(i) {
			v = i
			break
		}
	}
	if v == 0 {
		return nil, errors.New("qr: the text is too long")
	}
	q := newSymbol(v)
	q.drawFunction()
	q.drawData(codewords(data, v))
	if mask < 0 {
		best := -1
		for m := 0; m < 8; m++ {
			c := q.clone()
			c.applyMask(m)
			c.drawFormat(m)
			if p := c.penalty(); best < 0 || p < best {
				best, mask = p, m
			}
		}
	}
	q.applyMask(mask)
	q.drawFormat(mask)
	return q.mod, nil
}

// ---- the codewords ------------------------------------------------------------------------------------------------------------

type bitBuf []bool

func (b *bitBuf) put(v, n int) {
	for i := n - 1; i >= 0; i-- {
		*b = append(*b, v>>uint(i)&1 == 1)
	}
}

// codewords is the text in byte mode, padded to the size of version v, split into blocks with their error correction, interleaved.
func codewords(data []byte, v int) []byte {
	ver := versions[v]
	total := ver.dataCodewords()
	var b bitBuf
	b.put(0b0100, 4) // byte mode
	b.put(len(data), countBits(v))
	for _, c := range data {
		b.put(int(c), 8)
	}
	b.put(0, min(4, total*8-len(b))) // the terminator
	for len(b)%8 != 0 {
		b.put(0, 1)
	}
	for pad := 0xEC; len(b) < total*8; pad ^= 0xEC ^ 0x11 {
		b.put(pad, 8)
	}
	all := make([]byte, total)
	for i, on := range b {
		if on {
			all[i>>3] |= 0x80 >> uint(i&7)
		}
	}
	div := rsDivisor(ver.ec)
	var blocks, ecs [][]byte
	k := 0
	for _, g := range ver.groups {
		for n := 0; n < g[0]; n++ {
			blk := all[k : k+g[1]]
			k += g[1]
			blocks = append(blocks, blk)
			ecs = append(ecs, rsRemainder(blk, div))
		}
	}
	var out []byte
	for i := 0; ; i++ { // a codeword of each block in turn; the shorter blocks run out first
		added := false
		for _, blk := range blocks {
			if i < len(blk) {
				out = append(out, blk[i])
				added = true
			}
		}
		if !added {
			break
		}
	}
	for i := 0; i < ver.ec; i++ {
		for _, e := range ecs {
			out = append(out, e[i])
		}
	}
	return out
}

// gfMul multiplies in GF(256) modulo x^8 + x^4 + x^3 + x^2 + 1.
func gfMul(x, y byte) byte {
	var z byte
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x1D)
		z ^= ((y >> uint(i)) & 1) * x
	}
	return z
}

// rsDivisor is the generator polynomial of degree n, (x - a^0)(x - a^1)...(x - a^(n-1)), without its leading 1.
func rsDivisor(n int) []byte {
	r := make([]byte, n)
	r[n-1] = 1
	root := byte(1)
	for i := 0; i < n; i++ {
		for j := range r {
			r[j] = gfMul(r[j], root)
			if j+1 < len(r) {
				r[j] ^= r[j+1]
			}
		}
		root = gfMul(root, 2)
	}
	return r
}

// rsRemainder is the error correction of data: the remainder of data * x^n divided by the generator.
func rsRemainder(data, div []byte) []byte {
	r := make([]byte, len(div))
	for _, b := range data {
		f := b ^ r[0]
		copy(r, r[1:])
		r[len(r)-1] = 0
		for i := range r {
			r[i] ^= gfMul(div[i], f)
		}
	}
	return r
}

// ---- the symbol ---------------------------------------------------------------------------------------------------------------

type symbol struct {
	v, size int
	mod, fn [][]bool // the modules, and which of them belong to a pattern and carry no data
}

func newSymbol(v int) *symbol {
	size := 4*v + 17
	q := &symbol{v: v, size: size}
	for i := 0; i < size; i++ {
		q.mod = append(q.mod, make([]bool, size))
		q.fn = append(q.fn, make([]bool, size))
	}
	return q
}

func (q *symbol) clone() *symbol {
	c := &symbol{v: q.v, size: q.size}
	for i := range q.mod {
		c.mod = append(c.mod, append([]bool(nil), q.mod[i]...))
		c.fn = append(c.fn, append([]bool(nil), q.fn[i]...))
	}
	return c
}

// set draws a module of a pattern; x is the column and y the row.
func (q *symbol) set(x, y int, dark bool) {
	q.mod[y][x] = dark
	q.fn[y][x] = true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (q *symbol) drawFunction() {
	for i := 0; i < q.size; i++ { // the timing patterns
		q.set(6, i, i%2 == 0)
		q.set(i, 6, i%2 == 0)
	}
	q.finder(3, 3)
	q.finder(q.size-4, 3)
	q.finder(3, q.size-4)
	a := versions[q.v].align
	last := len(a) - 1
	for i, x := range a {
		for j, y := range a {
			if (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0) {
				continue // where the finder patterns are
			}
			q.alignment(x, y)
		}
	}
	q.drawFormat(0) // keeps the place; drawn again with the mask that is chosen
	q.drawVersion()
}

// finder draws a finder pattern with its light border (the separator).
func (q *symbol) finder(cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || y < 0 || x >= q.size || y >= q.size {
				continue
			}
			d := max(abs(dx), abs(dy))
			q.set(x, y, d != 2 && d != 4)
		}
	}
}

func (q *symbol) alignment(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			q.set(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
		}
	}
}

// formatBits are the 15 bits of the format: level M (00) and the mask, with their BCH code, masked with 101010000010010.
func formatBits(mask int) int {
	data := 0b00<<3 | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return (data<<10 | rem) ^ 0x5412
}

func (q *symbol) drawFormat(mask int) {
	bits := formatBits(mask)
	bit := func(i int) bool { return bits>>uint(i)&1 == 1 }
	// Beside the finder at the top left.
	for i := 0; i <= 5; i++ {
		q.set(8, i, bit(i))
	}
	q.set(8, 7, bit(6))
	q.set(8, 8, bit(7))
	q.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		q.set(14-i, 8, bit(i))
	}
	// The copy beside the other two finders.
	for i := 0; i < 8; i++ {
		q.set(q.size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		q.set(8, q.size-15+i, bit(i))
	}
	q.set(8, q.size-8, true) // the dark module, always there
}

// drawVersion draws the two blocks that name the version, from version 7 on.
func (q *symbol) drawVersion() {
	if q.v < 7 {
		return
	}
	rem := q.v
	for i := 0; i < 12; i++ {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	bits := q.v<<12 | rem
	for i := 0; i < 18; i++ {
		dark := bits>>uint(i)&1 == 1
		a, b := q.size-11+i%3, i/3
		q.set(a, b, dark)
		q.set(b, a, dark)
	}
}

// drawData puts the codewords in the free modules, two columns at a time from the right, up and down in turn. The column of the
// vertical timing pattern is skipped. Modules that are left over stay light.
func (q *symbol) drawData(data []byte) {
	i := 0
	for right := q.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < q.size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				y := vert
				if (right+1)&2 == 0 { // upward
					y = q.size - 1 - vert
				}
				if !q.fn[y][x] && i < len(data)*8 {
					q.mod[y][x] = data[i>>3]>>uint(7-i&7)&1 == 1
					i++
				}
			}
		}
	}
}

func maskBit(m, x, y int) bool {
	switch m {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (x/3+y/2)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	default:
		return ((x+y)%2+x*y%3)%2 == 0
	}
}

func (q *symbol) applyMask(m int) {
	for y := 0; y < q.size; y++ {
		for x := 0; x < q.size; x++ {
			if !q.fn[y][x] && maskBit(m, x, y) {
				q.mod[y][x] = !q.mod[y][x]
			}
		}
	}
}

// penalty scores a masked symbol by the four rules of the standard; the mask with the lowest score is the easiest to read.
func (q *symbol) penalty() int {
	n := q.size
	at := func(x, y int, rows bool) bool {
		if rows {
			return q.mod[y][x]
		}
		return q.mod[x][y]
	}
	score := 0
	finderLike := [][]bool{
		{true, false, true, true, true, false, true, false, false, false, false},
		{false, false, false, false, true, false, true, true, true, false, true},
	}
	for _, rows := range []bool{true, false} {
		for y := 0; y < n; y++ {
			run := 1
			for x := 1; x <= n; x++ {
				if x < n && at(x, y, rows) == at(x-1, y, rows) {
					run++
					continue
				}
				if run >= 5 {
					score += 3 + run - 5
				}
				run = 1
			}
			for x := 0; x+11 <= n; x++ {
				for _, p := range finderLike {
					match := true
					for k := range p {
						if at(x+k, y, rows) != p[k] {
							match = false
							break
						}
					}
					if match {
						score += 40
					}
				}
			}
		}
	}
	dark := 0
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if q.mod[y][x] {
				dark++
			}
			if x+1 < n && y+1 < n {
				c := q.mod[y][x]
				if q.mod[y][x+1] == c && q.mod[y+1][x] == c && q.mod[y+1][x+1] == c {
					score += 3
				}
			}
		}
	}
	score += abs(dark*100/(n*n)-50) / 5 * 10
	return score
}
