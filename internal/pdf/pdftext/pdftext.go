// Package pdftext extracts the text of the PDFs the receipt renderer writes,
// for tests: no external tool, and just enough PDF for typst's output
// (objects without object streams, Flate streams, Type0 fonts with a
// ToUnicode CMap and two-byte codes, text in literal or hex strings).
package pdftext

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

var (
	objRe      = regexp.MustCompile(`(?s)(\d+) 0 obj\s*(.*?)endobj`)
	lengthRe   = regexp.MustCompile(`/Length (\d+)`)
	refRe      = regexp.MustCompile(`(\d+) 0 R`)
	fontsRe    = regexp.MustCompile(`/Font\s*<<(.*?)>>`)
	fontRefRe  = regexp.MustCompile(`/([A-Za-z0-9_.+-]+)\s*(\d+) 0 R`)
	toUniRe    = regexp.MustCompile(`/ToUnicode (\d+) 0 R`)
	resRefRe   = regexp.MustCompile(`/Resources (\d+) 0 R`)
	contentsRe = regexp.MustCompile(`/Contents\s*(\[[^\]]*\]|\d+ 0 R)`)
	hexRe      = regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
)

type object struct {
	dict   string
	stream []byte // decompressed; nil without a stream
}

// Text returns the text of every page, one line per text object.
func Text(pdf []byte) (string, error) {
	objs, err := objects(pdf)
	if err != nil {
		return "", err
	}

	var out strings.Builder

	for _, n := range sortedKeys(objs) {
		o := objs[n]
		if !strings.Contains(o.dict, "/Type/Page/") && !strings.HasSuffix(o.dict, "/Type/Page") &&
			!strings.Contains(o.dict, "/Type /Page ") {
			continue
		}

		fonts, err := pageFonts(objs, o.dict)
		if err != nil {
			return "", err
		}

		m := contentsRe.FindStringSubmatch(o.dict)
		if m == nil {
			continue
		}

		for _, ref := range refRe.FindAllStringSubmatch(m[1], -1) {
			c, ok := objs[atoi(ref[1])]
			if !ok {
				return "", fmt.Errorf("pdftext: missing content stream %s", ref[1])
			}

			show(&out, c.stream, fonts)
		}
	}

	return out.String(), nil
}

func objects(pdf []byte) (map[int]object, error) {
	out := map[int]object{}

	for _, m := range objRe.FindAllSubmatchIndex(pdf, -1) {
		n := atoi(string(pdf[m[2]:m[3]]))
		body := pdf[m[4]:m[5]]

		i := bytes.Index(body, []byte("stream"))
		if i < 0 || bytes.HasPrefix(body, []byte("[")) && i > bytes.IndexByte(body, ']') {
			out[n] = object{dict: string(bytes.TrimSpace(body))}

			continue
		}

		dict := string(bytes.TrimSpace(body[:i]))
		data := bytes.TrimLeft(body[i+len("stream"):], "\r")
		data = bytes.TrimPrefix(data, []byte("\n"))

		if l := lengthRe.FindStringSubmatch(dict); l != nil && atoi(l[1]) <= len(data) {
			data = data[:atoi(l[1])]
		}

		if strings.Contains(dict, "/FlateDecode") {
			r, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, fmt.Errorf("pdftext: object %d: %w", n, err)
			}

			if data, err = io.ReadAll(r); err != nil {
				return nil, fmt.Errorf("pdftext: object %d: %w", n, err)
			}
		}

		out[n] = object{dict: dict, stream: data}
	}

	if len(out) == 0 {
		return nil, errors.New("pdftext: no objects")
	}

	return out, nil
}

// pageFonts maps the font names of a page's resources to their code →
// text tables.
func pageFonts(objs map[int]object, page string) (map[string]map[uint16]string, error) {
	res := page
	if m := resRefRe.FindStringSubmatch(page); m != nil {
		res = objs[atoi(m[1])].dict
	}

	out := map[string]map[uint16]string{}

	m := fontsRe.FindStringSubmatch(res)
	if m == nil {
		return out, nil
	}

	for _, f := range fontRefRe.FindAllStringSubmatch(m[1], -1) {
		font := objs[atoi(f[2])]

		u := toUniRe.FindStringSubmatch(font.dict)
		if u == nil {
			return nil, fmt.Errorf("pdftext: font %s has no ToUnicode map", f[1])
		}

		out[f[1]] = cmap(objs[atoi(u[1])].stream)
	}

	return out, nil
}

// cmap reads the bfchar and bfrange entries of a ToUnicode CMap.
func cmap(data []byte) map[uint16]string {
	out := map[uint16]string{}
	s := string(data)

	for _, block := range between(s, "beginbfchar", "endbfchar") {
		hs := hexRe.FindAllStringSubmatch(block, -1)
		for i := 0; i+1 < len(hs); i += 2 {
			out[hex16(hs[i][1])] = utf16Hex(hs[i+1][1])
		}
	}

	for _, block := range between(s, "beginbfrange", "endbfrange") {
		hs := hexRe.FindAllStringSubmatch(block, -1)
		for i := 0; i+2 < len(hs); i += 3 {
			lo, hi := hex16(hs[i][1]), hex16(hs[i+1][1])
			dst := []rune(utf16Hex(hs[i+2][1]))

			for c := lo; c <= hi && c >= lo && len(dst) > 0; c++ {
				r := append([]rune{}, dst...)
				r[len(r)-1] += rune(c - lo)
				out[c] = string(r)
			}
		}
	}

	return out
}

func between(s, begin, end string) []string {
	var out []string

	for {
		i := strings.Index(s, begin)
		if i < 0 {
			return out
		}

		s = s[i+len(begin):]

		j := strings.Index(s, end)
		if j < 0 {
			return out
		}

		out = append(out, s[:j])
		s = s[j+len(end):]
	}
}

// show writes the strings a content stream shows, decoded with the current
// font, a newline after each text object.
func show(out *strings.Builder, content []byte, fonts map[string]map[uint16]string) {
	var (
		font     map[uint16]string
		lastName string
	)

	for i := 0; i < len(content); i++ {
		switch c := content[i]; {
		case c == '/':
			j := i + 1
			for j < len(content) && !isDelim(content[j]) {
				j++
			}

			lastName = string(content[i+1 : j])
			i = j - 1
		case c == 'T' && i+1 < len(content) && content[i+1] == 'f':
			if f, ok := fonts[lastName]; ok {
				font = f
			}
		case c == 'E' && i+1 < len(content) && content[i+1] == 'T':
			out.WriteByte('\n')
		case c == '(':
			s, j := literal(content, i+1)
			decode(out, s, font)
			i = j
		case c == '<' && i+1 < len(content) && content[i+1] != '<':
			j := bytes.IndexByte(content[i:], '>')
			if j < 0 {
				return
			}

			decode(out, hexBytes(string(content[i+1:i+j])), font)
			i += j
		case c == '<':
			i++ // a dictionary
		}
	}
}

func isDelim(c byte) bool { return strings.IndexByte(" \t\r\n/[]()<>{}%", c) >= 0 }

// literal reads a literal string starting after its "(" at i and returns it
// and the index of its ")".
func literal(b []byte, i int) ([]byte, int) {
	var (
		out   []byte
		depth = 1
	)

	for ; i < len(b); i++ {
		c := b[i]

		switch {
		case c == '\\' && i+1 < len(b):
			i++

			switch e := b[i]; e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '0', '1', '2', '3', '4', '5', '6', '7':
				v, n := 0, 0
				for n < 3 && i < len(b) && b[i] >= '0' && b[i] <= '7' {
					v = v*8 + int(b[i]-'0')
					i++
					n++
				}

				i--

				out = append(out, byte(v))
			default:
				out = append(out, e)
			}
		case c == '(':
			depth++

			out = append(out, c)
		case c == ')':
			depth--
			if depth == 0 {
				return out, i
			}

			out = append(out, c)
		default:
			out = append(out, c)
		}
	}

	return out, i
}

func decode(out *strings.Builder, s []byte, font map[uint16]string) {
	for i := 0; i+1 < len(s); i += 2 {
		code := uint16(s[i])<<8 | uint16(s[i+1])
		if t, ok := font[code]; ok {
			out.WriteString(t)
		} else {
			out.WriteRune('�')
		}
	}
}

func hexBytes(h string) []byte {
	out := make([]byte, 0, len(h)/2)
	for i := 0; i+1 < len(h); i += 2 {
		v, _ := strconv.ParseUint(h[i:i+2], 16, 8)
		out = append(out, byte(v))
	}

	return out
}

// hex16 parses a code of at most 16 bits; anything else is 0.
func hex16(h string) uint16 {
	v, _ := strconv.ParseUint(h, 16, 16)

	return uint16(v)
}

func utf16Hex(h string) string {
	b := hexBytes(h)
	u := make([]uint16, 0, len(b)/2)

	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
	}

	return string(utf16.Decode(u))
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))

	return n
}

func sortedKeys(m map[int]object) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}

	return keys
}
