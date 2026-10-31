package pdftext_test

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/cwbudde/studio-schedule/internal/pdf/pdftext"
)

// pdfFile assembles a hand-written PDF: just the "N 0 obj … endobj" objects
// pdftext reads, between a header and a trailer.
type pdfFile struct{ buf bytes.Buffer }

func newPDF() *pdfFile {
	p := &pdfFile{}
	p.buf.WriteString("%PDF-1.7\n")

	return p
}

func (p *pdfFile) obj(n int, body string) *pdfFile {
	fmt.Fprintf(&p.buf, "%d 0 obj\n%s\nendobj\n", n, body)

	return p
}

// stream writes a stream object with the given dict entries and a /Length
// of the data.
func (p *pdfFile) stream(n int, dict string, data []byte) *pdfFile {
	fmt.Fprintf(&p.buf, "%d 0 obj\n<<%s /Length %d>>\nstream\n", n, dict, len(data))
	p.buf.Write(data)
	p.buf.WriteString("\nendstream\nendobj\n")

	return p
}

// flate writes a /FlateDecode stream of data, compressed here.
func (p *pdfFile) flate(t *testing.T, n int, dict string, data []byte) *pdfFile {
	t.Helper()

	return p.stream(n, dict+" /Filter /FlateDecode", deflate(t, data))
}

func (p *pdfFile) bytes() []byte {
	return append(bytes.Clone(p.buf.Bytes()), "trailer\n<</Root 1 0 R>>\n%%EOF\n"...)
}

func deflate(t *testing.T, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// toUnicode is a ToUnicode CMap with one bfchar entry per code, in code
// order, and the given extra lines (e.g. a bfrange block) after it.
func toUnicode(codes map[uint16]string, extra ...string) []byte {
	var b strings.Builder

	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CMapName /Test-UCS def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")

	if len(codes) > 0 {
		keys := make([]uint16, 0, len(codes))
		for c := range codes {
			keys = append(keys, c)
		}

		slices.Sort(keys)
		fmt.Fprintf(&b, "%d beginbfchar\n", len(keys))

		for _, c := range keys {
			fmt.Fprintf(&b, "<%04X> <%s>\n", c, utf16Of(codes[c]))
		}

		b.WriteString("endbfchar\n")
	}

	for _, e := range extra {
		b.WriteString(e + "\n")
	}

	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")

	return []byte(b.String())
}

func utf16Of(s string) string {
	var b strings.Builder
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&b, "%04X", u)
	}

	return b.String()
}

// latin maps the codes 3, 4 and 5 to A, B and C, the way typst numbers the
// glyphs of a subset font.
var latin = map[uint16]string{3: "A", 4: "B", 5: "C"}

// onePage is a PDF with one page (object 3) whose font F1 (object 5) has the
// CMap, and whose content stream (object 6) is content.
func onePage(cmap []byte, content string) []byte {
	return newPDF().
		obj(1, "<</Type /Catalog /Pages 2 0 R>>").
		obj(2, "<</Type /Pages /Kids [3 0 R] /Count 1>>").
		obj(3, "<</Type /Page /Parent 2 0 R /Resources 4 0 R /Contents 6 0 R>>").
		obj(4, "<</Font <</F1 5 0 R>>>>").
		obj(5, "<</Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Fictional /ToUnicode 7 0 R>>").
		stream(6, "", []byte(content)).
		stream(7, "", cmap).
		bytes()
}

func text(t *testing.T, pdf []byte) string {
	t.Helper()

	got, err := pdftext.Text(pdf)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}

	return got
}

func textErr(t *testing.T, pdf []byte, want string) {
	t.Helper()

	got, err := pdftext.Text(pdf)
	if err == nil {
		t.Fatalf("Text = %q, want an error containing %q", got, want)
	}

	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Text error = %q, want it to contain %q", err, want)
	}
}

func TestTextOfOnePage(t *testing.T) {
	got := text(t, onePage(toUnicode(latin), "BT /F1 12 Tf 72 720 Td <00030004> Tj (\x00\x05) Tj ET"))
	if got != "ABC\n" {
		t.Fatalf("Text = %q, want %q", got, "ABC\n")
	}
}

func TestTextFlateStreams(t *testing.T) {
	pdf := newPDF().
		obj(3, "<</Type /Page /Resources <</Font <</F1 5 0 R>>>> /Contents 6 0 R>>").
		obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
		flate(t, 6, "", []byte("BT /F1 1 Tf <000300040005> Tj ET")).
		flate(t, 7, "", toUnicode(latin)).
		bytes()

	if got := text(t, pdf); got != "ABC\n" {
		t.Fatalf("Text = %q, want %q", got, "ABC\n")
	}
}

func TestTextMalformedInput(t *testing.T) {
	for name, pdf := range map[string][]byte{
		"empty":        nil,
		"not a pdf":    []byte("not a pdf"),
		"no endobj":    []byte("%PDF-1.7\n1 0 obj\n<</Type /Catalog>>\n"),
		"generation 1": []byte("1 1 obj\n<</Type /Catalog>>\nendobj\n"),
	} {
		t.Run(name, func(t *testing.T) { textErr(t, pdf, "pdftext: no objects") })
	}
}

func TestTextBadFlateStream(t *testing.T) {
	zipped := deflate(t, []byte("BT /F1 1 Tf <0003> Tj ET"))

	for name, data := range map[string][]byte{
		"not zlib":  []byte("plain text, no zlib header"),
		"truncated": zipped[:len(zipped)/2],
	} {
		t.Run(name, func(t *testing.T) {
			pdf := newPDF().
				obj(3, "<</Type /Page /Contents 6 0 R>>").
				stream(6, " /Filter /FlateDecode", data).
				bytes()

			textErr(t, pdf, "pdftext: object 6: ")
		})
	}
}

func TestTextFontWithoutToUnicode(t *testing.T) {
	for name, page := range map[string]string{
		"with contents":    "<</Type /Page /Resources 4 0 R /Contents 6 0 R>>",
		"without contents": "<</Type /Page /Resources 4 0 R>>",
	} {
		t.Run(name, func(t *testing.T) {
			pdf := newPDF().
				obj(3, page).
				obj(4, "<</Font <</F1 5 0 R /F2 8 0 R>>>>").
				obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
				stream(6, "", []byte("BT /F1 1 Tf <0003> Tj ET")).
				stream(7, "", toUnicode(latin)).
				obj(8, "<</Type /Font /Subtype /Type0 /BaseFont /Fictional-Bold>>").
				bytes()

			textErr(t, pdf, "pdftext: font F2 has no ToUnicode map")
		})
	}
}

func TestTextFontObjectMissing(t *testing.T) {
	// A font reference to an object that is not there reads as a font
	// without ToUnicode.
	pdf := newPDF().
		obj(3, "<</Type /Page /Resources <</Font <</F1 50 0 R>>>> /Contents 6 0 R>>").
		stream(6, "", []byte("BT /F1 1 Tf <0003> Tj ET")).
		bytes()

	textErr(t, pdf, "pdftext: font F1 has no ToUnicode map")
}

func TestTextToUnicodeObjectMissing(t *testing.T) {
	// A ToUnicode reference to an object that is not there is an empty map.
	pdf := newPDF().
		obj(3, "<</Type /Page /Resources <</Font <</F1 5 0 R>>>> /Contents 6 0 R>>").
		obj(5, "<</Type /Font /ToUnicode 70 0 R>>").
		stream(6, "", []byte("BT /F1 1 Tf <0003> Tj ET")).
		bytes()

	if got := text(t, pdf); got != "\uFFFD\n" {
		t.Fatalf("Text = %q, want %q", got, "\uFFFD\n")
	}
}

func TestTextPageWithoutContents(t *testing.T) {
	pdf := newPDF().
		obj(3, "<</Type /Page /Resources 4 0 R>>").
		obj(4, "<</Font <</F1 5 0 R>>>>").
		obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
		stream(7, "", toUnicode(latin)).
		obj(8, "<</Type /Page /Resources 4 0 R /Contents 9 0 R>>").
		stream(9, "", []byte("BT /F1 1 Tf <0005> Tj ET")).
		bytes()

	// The empty page 3 is skipped; page 8 still counts.
	if got := text(t, pdf); got != "C\n" {
		t.Fatalf("Text = %q, want %q", got, "C\n")
	}
}

func TestTextMissingContentStream(t *testing.T) {
	for name, contents := range map[string]string{
		"single": "/Contents 99 0 R",
		"array":  "/Contents [6 0 R 99 0 R]",
	} {
		t.Run(name, func(t *testing.T) {
			pdf := newPDF().
				obj(3, "<</Type /Page "+contents+">>").
				stream(6, "", []byte("BT ET")).
				bytes()

			textErr(t, pdf, "pdftext: missing content stream 99")
		})
	}
}

func TestTextResourcesWithoutFont(t *testing.T) {
	for name, resources := range map[string]string{
		"inline":         "<</ProcSet [/PDF /Text]>>",
		"indirect":       "4 0 R",
		"missing object": "40 0 R",
		// Pinned, not wanted: a font dictionary across lines is not found;
		// typst writes it on one line.
		"font dict across lines": "<</Font <<\n/F1 5 0 R\n>>>>",
	} {
		t.Run(name, func(t *testing.T) {
			pdf := newPDF().
				obj(3, "<</Type /Page /Resources "+resources+" /Contents 6 0 R>>").
				obj(4, "<</ProcSet [/PDF /Text]>>").
				obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
				stream(6, "", []byte("BT /F1 1 Tf <00030004> Tj (\x00\x05) Tj ET")).
				stream(7, "", toUnicode(latin)).
				bytes()

			// Without a font, every two-byte code is the replacement character.
			if got, want := text(t, pdf), "\uFFFD\uFFFD\uFFFD\n"; got != want {
				t.Fatalf("Text = %q, want %q", got, want)
			}
		})
	}
}

func TestTextPageDetection(t *testing.T) {
	for _, tc := range []struct {
		name string
		dict string
		want string
	}{
		{"spaced", "<< /Type /Page /Contents 6 0 R /Resources 4 0 R >>", "A\n"},
		{"compact", "<</Type/Page/Contents 6 0 R/Resources 4 0 R>>", "A\n"},
		{"suffix", "/Contents 6 0 R /Resources 4 0 R /Type/Page", "A\n"},
		{"pages node", "<< /Type /Pages /Contents 6 0 R /Resources 4 0 R >>", ""},
		{"compact pages node", "<</Type/Pages/Contents 6 0 R/Resources 4 0 R>>", ""},
		{"no type", "<< /Contents 6 0 R /Resources 4 0 R >>", ""},
		// Pinned, not wanted: a /Type /Page right before the dictionary's
		// end (or a line break) is not recognized; typst's pages match
		// one of the forms above.
		{"spaced at end", "<< /Contents 6 0 R /Resources 4 0 R /Type /Page>>", ""},
		{"compact at end", "<</Contents 6 0 R/Resources 4 0 R/Type/Page>>", ""},
		{"line break", "<<\n/Type /Page\n/Contents 6 0 R\n/Resources 4 0 R\n>>", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pdf := newPDF().
				obj(3, tc.dict).
				obj(4, "<</Font <</F1 5 0 R>>>>").
				obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
				stream(6, "", []byte("BT /F1 1 Tf <0003> Tj ET")).
				stream(7, "", toUnicode(latin)).
				bytes()

			if got := text(t, pdf); got != tc.want {
				t.Fatalf("Text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTextContentsArray(t *testing.T) {
	pdf := newPDF().
		obj(3, "<</Type /Page /Resources 4 0 R /Contents [ 9 0 R 6 0 R ]>>").
		obj(4, "<</Font <</F1 5 0 R>>>>").
		obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
		stream(6, "", []byte("BT /F1 1 Tf <0004> Tj ET")).
		stream(7, "", toUnicode(latin)).
		stream(9, "", []byte("BT /F1 1 Tf <0003> Tj ET")).
		bytes()

	// Both streams, in the order of the array, not of the object numbers.
	if got := text(t, pdf); got != "A\nB\n" {
		t.Fatalf("Text = %q, want %q", got, "A\nB\n")
	}
}

func TestTextContentsIndirectArray(t *testing.T) {
	// Pinned, not wanted: /Contents pointing at an array object reads that
	// array object as the (empty) content stream; typst writes the stream
	// reference directly.
	pdf := newPDF().
		obj(3, "<</Type /Page /Resources 4 0 R /Contents 8 0 R>>").
		obj(4, "<</Font <</F1 5 0 R>>>>").
		obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
		stream(6, "", []byte("BT /F1 1 Tf <0003> Tj ET")).
		stream(7, "", toUnicode(latin)).
		obj(8, "[6 0 R]").
		bytes()

	if got := text(t, pdf); got != "" {
		t.Fatalf("Text = %q, want %q", got, "")
	}
}

func TestTextPagesInObjectOrder(t *testing.T) {
	pdf := newPDF().
		obj(2, "<</Type /Pages /Kids [12 0 R 3 0 R] /Count 2>>").
		obj(3, "<</Type /Page /Resources 4 0 R /Contents 6 0 R>>").
		obj(4, "<</Font <</F1 5 0 R>>>>").
		obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
		stream(6, "", []byte("BT /F1 1 Tf <0003> Tj ET")).
		stream(7, "", toUnicode(latin)).
		obj(12, "<</Type /Page /Resources 4 0 R /Contents 13 0 R>>").
		stream(13, "", []byte("BT /F1 1 Tf <0004> Tj ET BT /F1 1 Tf <0005> Tj ET")).
		bytes()

	// The pages come in object-number order (3, then 12), not in the order
	// of /Kids; one line per text object.
	if got := text(t, pdf); got != "A\nB\nC\n" {
		t.Fatalf("Text = %q, want %q", got, "A\nB\nC\n")
	}
}

func TestTextStreamLength(t *testing.T) {
	content := "BT /F1 1 Tf <0003> Tj ET"

	for _, tc := range []struct {
		name   string
		length string
		want   string
	}{
		// The data after /Length is not part of the stream.
		{"shorter", fmt.Sprint(len(content)), "A\n"},
		{"cuts the text", fmt.Sprint(strings.Index(content, " ET")), "A"},
		// Longer than the data: the whole data, up to endobj.
		{"longer", "100000", "A\nB\n"},
		// Pinned, not wanted: an indirect length is read as the object
		// number, here 5 bytes ("BT /F"); typst writes direct lengths.
		{"indirect", "5 0 R", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			fmt.Fprintf(&b, "6 0 obj\n<</Length %s>>\nstream\r\n%s <0004> Tj ET\nendstream\nendobj\n",
				tc.length, content)

			pdf := newPDF().
				obj(3, "<</Type /Page /Resources <</Font <</F1 5 0 R>>>> /Contents 6 0 R>>").
				obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
				stream(7, "", toUnicode(latin)).
				bytes()
			pdf = append(pdf, b.Bytes()...)

			if got := text(t, pdf); got != tc.want {
				t.Fatalf("Text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTextBFRange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bfrange string
		content string
		want    string
	}{
		{
			name:    "consecutive",
			bfrange: "1 beginbfrange\n<0003> <0005> <0041>\nendbfrange",
			content: "<00020003000400050006>",
			want:    "\uFFFDABC\uFFFD",
		},
		{
			// The last rune of a multi-rune destination counts up.
			name:    "multi-rune destination",
			bfrange: "1 beginbfrange\n<0010> <0012> <00660066>\nendbfrange",
			content: "<001000110012>",
			want:    "fffgfh",
		},
		{
			name:    "surrogate pair",
			bfrange: "1 beginbfrange\n<0020> <0021> <D83DDE00>\nendbfrange",
			content: "<00200021>",
			want:    "\U0001F600\U0001F601",
		},
		{
			// The guard against the uint16 wrap-around ends the range.
			name:    "range up to FFFF",
			bfrange: "1 beginbfrange\n<FFFE> <FFFF> <0058>\nendbfrange",
			content: "<FFFEFFFF0000>",
			want:    "XY\uFFFD",
		},
		{
			name:    "single code",
			bfrange: "1 beginbfrange\n<0007> <0007> <005A>\nendbfrange",
			content: "<00070008>",
			want:    "Z\uFFFD",
		},
		{
			name:    "reversed range",
			bfrange: "1 beginbfrange\n<0009> <0007> <005A>\nendbfrange",
			content: "<000700080009>",
			want:    "\uFFFD\uFFFD\uFFFD",
		},
		{
			// A destination shorter than one UTF-16 unit maps nothing.
			name:    "empty destination",
			bfrange: "1 beginbfrange\n<0007> <0008> <5A>\nendbfrange",
			content: "<00070008>",
			want:    "\uFFFD\uFFFD",
		},
		{
			name: "two blocks",
			bfrange: "1 beginbfrange\n<0007> <0008> <0061>\nendbfrange\n" +
				"1 beginbfrange\n<0009> <0009> <007A>\nendbfrange",
			content: "<000700080009>",
			want:    "abz",
		},
		{
			// Pinned, not wanted: the array form of a destination is read
			// as triples of hex strings, so its first entry starts a
			// consecutive range; typst writes no arrays.
			name:    "array destination",
			bfrange: "1 beginbfrange\n<0007> <0009> [<005A> <0041> <004D>]\nendbfrange",
			content: "<000700080009>",
			want:    "Z[\\",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pdf := onePage(toUnicode(nil, tc.bfrange), "BT /F1 1 Tf "+tc.content+" Tj ET")
			if got := text(t, pdf); got != tc.want+"\n" {
				t.Fatalf("Text = %q, want %q", got, tc.want+"\n")
			}
		})
	}
}

func TestTextBFCharAndRangeTogether(t *testing.T) {
	cmap := toUnicode(map[uint16]string{3: "A", 0x20: "ﬁ"},
		"1 beginbfrange\n<0004> <0005> <0042>\nendbfrange")

	got := text(t, onePage(cmap, "BT /F1 1 Tf <0003000400050020> Tj ET"))
	if got != "ABCﬁ\n" {
		t.Fatalf("Text = %q, want %q", got, "ABCﬁ\n")
	}
}

func TestTextUnclosedCMapBlock(t *testing.T) {
	cmap := []byte("begincmap\n1 beginbfchar\n<0003> <0041>\nendbfchar\n" +
		"1 beginbfchar\n<0004> <0042>\n" + // never closed: ignored
		"endcmap\n")

	if got := text(t, onePage(cmap, "BT /F1 1 Tf <00030004> Tj ET")); got != "A\uFFFD\n" {
		t.Fatalf("Text = %q, want %q", got, "A\uFFFD\n")
	}

	cmap = []byte("1 beginbfrange\n<0003> <0004> <0041>\n") // never closed
	if got := text(t, onePage(cmap, "BT /F1 1 Tf <00030004> Tj ET")); got != "\uFFFD\uFFFD\n" {
		t.Fatalf("Text = %q, want %q", got, "\uFFFD\uFFFD\n")
	}
}

func TestTextBFCharOddEntry(t *testing.T) {
	// A trailing source code without a destination is ignored.
	cmap := []byte("2 beginbfchar\n<0003> <0041>\n<0004>\nendbfchar\n")

	if got := text(t, onePage(cmap, "BT /F1 1 Tf <00030004> Tj ET")); got != "A\uFFFD\n" {
		t.Fatalf("Text = %q, want %q", got, "A\uFFFD\n")
	}
}

func TestTextHexStrings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{"one code", "BT /F1 1 Tf <0003> Tj ET", "A\n"},
		{"lower case", "BT /F1 1 Tf <000a000B> Tj ET", "jk\n"},
		{"in an array", "BT /F1 1 Tf [<0003> -120 <0004>] TJ ET", "AB\n"},
		// An odd trailing byte (or digit) is dropped.
		{"odd bytes", "BT /F1 1 Tf <000300> Tj ET", "A\n"},
		{"odd digits", "BT /F1 1 Tf <00030> Tj ET", "A\n"},
		{"empty", "BT /F1 1 Tf <> Tj ET", "\n"},
		// Unterminated: the rest of the stream is not read.
		{"unterminated", "BT /F1 1 Tf <0003> Tj <0004", "A"},
		// Unclosed before a later string: it runs to that string's ">",
		// the operators in between read as (non-hex, so zero) bytes.
		{"unclosed before another", "BT /F1 1 Tf <0003 Tj ET BT <0004> Tj ET", "A\uFFFD\uFFFD\uFFFD\n"},
		// A dictionary (marked content) is not a string.
		{"marked content", "/Span <</MCID 0>> BDC BT /F1 1 Tf <0003> Tj ET EMC", "A\n"},
		{"two text objects", "BT /F1 1 Tf <0003> Tj ET\nBT /F1 1 Tf <0004> Tj ET", "A\nB\n"},
		{"empty text object", "BT ET", "\n"},
		{"no text object", "q 1 0 0 1 0 0 cm Q", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			font := map[uint16]string{3: "A", 4: "B", 0x0A: "j", 0x0B: "k"}
			if got := text(t, onePage(toUnicode(font), tc.content)); got != tc.want {
				t.Fatalf("Text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTextLiteralStrings(t *testing.T) {
	// A literal string's bytes are two-byte codes too; the font maps the
	// codes the escapes below produce to letters.
	font := map[uint16]string{
		'A'<<8 | 'B':   "1",
		'\n'<<8 | '\r': "2",
		'\t'<<8 | '\b': "3",
		'\f'<<8 | '\\': "4",
		'('<<8 | ')':   "5",
		'x'<<8 | '(':   "6",
		'y'<<8 | ')':   "7",
		0x0003:         "8",
		'S'<<8 | 'q':   "9",
		'a'<<8 | 'b':   "0",
		'q'<<8 | '\n':  "+",
		0xFF00:         "-",
		0x0333:         "#",
	}

	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{"plain", `(AB)`, "1"},
		{"newline and return", `(\n\r)`, "2"},
		{"tab and backspace", `(\t\b)`, "3"},
		{"form feed and backslash", `(\f\\)`, "4"},
		{"escaped parentheses", `(\(\))`, "5"},
		{"three-digit octal", `(\101\102)`, "1"},
		{"short octal", `(\0\3)`, "8"},
		{"octal of three digits at most", `(\0033)`, "#"},
		{"octal wraps to a byte", `(\777\0)`, "-"},
		{"unknown escape", `(\S\q)`, "9"},
		{"nested parentheses", `(x(y))`, "67"},
		// Pinned: a backslash before a line break is not a line
		// continuation; the line break stays in the string.
		{"escaped line break", "(q\\\n)", "+"},
		// Unterminated: the string runs to the end of the stream, its odd
		// last byte dropped.
		{"unterminated", `(abc`, "0"},
		{"unterminated with backslash", `(ab\`, "0"},
		{"unterminated nested", `(ab(`, "0"},
		{"unterminated octal", `(\0\3`, "8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "BT /F1 1 Tf " + tc.content + " Tj ET"
			want := tc.want + "\n"

			if strings.HasPrefix(tc.name, "unterminated") {
				content = "BT /F1 1 Tf " + tc.content
				want = tc.want
			}

			if got := text(t, onePage(toUnicode(font), content)); got != want {
				t.Fatalf("Text = %q, want %q", got, want)
			}
		})
	}
}

func TestTextCodeMissingFromFont(t *testing.T) {
	got := text(t, onePage(toUnicode(latin), "BT /F1 1 Tf <0003FFFF00040099> Tj ET"))
	if got != "A\uFFFDB\uFFFD\n" {
		t.Fatalf("Text = %q, want %q", got, "A\uFFFDB\uFFFD\n")
	}
}

func TestTextFontSelection(t *testing.T) {
	pdf := newPDF().
		obj(3, "<</Type /Page /Resources 4 0 R /Contents 6 0 R>>").
		obj(4, "<</ProcSet [/PDF /Text] /Font <</F1 5 0 R /F2 8 0 R>>>>").
		obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
		stream(6, "", []byte(
			"BT <0003> Tj "+ // no font yet
				"/F1 1 Tf <0003> Tj "+
				"/F9 1 Tf <0003> Tj "+ // unknown: F1 stays
				"/F2 1 Tf <0003> Tj "+
				"/F1 1 Tf <0003> Tj ET")).
		stream(7, "", toUnicode(latin)).
		obj(8, "<</Type /Font /ToUnicode 9 0 R>>").
		stream(9, "", toUnicode(map[uint16]string{3: "z"})).
		bytes()

	if got := text(t, pdf); got != "\uFFFDAAzA\n" {
		t.Fatalf("Text = %q, want %q", got, "\uFFFDAAzA\n")
	}
}

func TestTextFontPerPage(t *testing.T) {
	// The font is chosen per content stream: the second page starts
	// without one, and each page reads its own resources.
	pdf := newPDF().
		obj(3, "<</Type /Page /Resources <</Font <</F1 5 0 R>>>> /Contents 6 0 R>>").
		obj(5, "<</Type /Font /ToUnicode 7 0 R>>").
		stream(6, "", []byte("BT /F1 1 Tf <0003> Tj ET")).
		stream(7, "", toUnicode(latin)).
		obj(10, "<</Type /Page /Resources <</Font <</F1 11 0 R>>>> /Contents 13 0 R>>").
		obj(11, "<</Type /Font /ToUnicode 12 0 R>>").
		stream(12, "", toUnicode(map[uint16]string{3: "q"})).
		stream(13, "", []byte("BT <0003> Tj /F1 1 Tf <0003> Tj ET")).
		bytes()

	if got := text(t, pdf); got != "A\n\uFFFDq\n" {
		t.Fatalf("Text = %q, want %q", got, "A\n\uFFFDq\n")
	}
}
