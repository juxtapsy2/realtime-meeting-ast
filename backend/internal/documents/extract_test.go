package documents

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestExtractText(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		data     string
		wantMime string
	}{
		{"plain text", "notes.txt", "Agenda item one", "text/plain"},
		{"csv", "report.csv", "item,status\r\nCR_002,open", "text/csv"},
		{"markdown", "minutes.md", "# MoM\n- decision", "text/markdown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Extract(tt.filename, []byte(tt.data))
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if doc.Mime != tt.wantMime {
				t.Errorf("Mime = %q, want %q", doc.Mime, tt.wantMime)
			}
			wantText := strings.ReplaceAll(tt.data, "\r\n", "\n")
			if doc.Text != wantText {
				t.Errorf("Text = %q", doc.Text)
			}
			if doc.Chars != len([]rune(wantText)) {
				t.Errorf("Chars = %d, want %d", doc.Chars, len([]rune(wantText)))
			}
		})
	}
}

func TestExtractUnsupported(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		data     []byte
	}{
		{"legacy doc", "report.doc", append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 64)...)},
		{"random binary", "blob.bin", []byte{0x01, 0x02, 0x03, 0x00, 0xFF, 0xFE}},
		{"empty zip", "thing.zip", zipBytes(t, map[string]string{"readme.txt": "hello"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Extract(tt.filename, tt.data)
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
		})
	}
}

func TestExtractEmpty(t *testing.T) {
	if _, err := Extract("empty.txt", nil); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v, want ErrEmpty", err)
	}
}

func TestExtractDocx(t *testing.T) {
	documentXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:r><w:t>Agenda</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Item </w:t><w:t>one</w:t></w:r><w:br/><w:r><w:t>next line</w:t></w:r></w:p>
<w:p><w:r><w:t>Decide on CR_002 by Friday</w:t></w:r></w:p>
</w:body></w:document>`

	doc, err := Extract("agenda.docx", zipBytes(t, map[string]string{
		"[Content_Types].xml":   `<Types/>`,
		"word/document.xml":     documentXML,
		"word/_rels/x.xml.rels": `<Relationships/>`,
	}))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	want := "Agenda\nItem one\nnext line\nDecide on CR_002 by Friday\n"
	if doc.Text != want {
		t.Errorf("Text = %q, want %q", doc.Text, want)
	}
	if doc.Mime != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Errorf("Mime = %q", doc.Mime)
	}
}

func TestExtractXlsx(t *testing.T) {
	shared := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="2" uniqueCount="2">
<si><t>Item</t></si><si><t>Status</t></si></sst>`
	sheet := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
<row r="2"><c r="A2"><v>42</v></c><c r="B2" t="inlineStr"><is><t>note</t></is></c></row>
<row r="3"><c r="A3" t="b"><v>1</v></c></row>
</sheetData></worksheet>`

	doc, err := Extract("report.xlsx", zipBytes(t, map[string]string{
		"[Content_Types].xml":      `<Types/>`,
		"xl/workbook.xml":          `<workbook/>`,
		"xl/sharedStrings.xml":     shared,
		"xl/worksheets/sheet1.xml": sheet,
	}))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	want := "Item\tStatus\n42\tnote\n"
	if doc.Text != want {
		t.Errorf("Text = %q, want %q", doc.Text, want)
	}
}

func TestExtractXlsxWithoutSharedStrings(t *testing.T) {
	sheet := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData><row r="1"><c r="A1"><v>7</v></c></row></sheetData></worksheet>`

	doc, err := Extract("n.xlsx", zipBytes(t, map[string]string{
		"xl/workbook.xml":          `<workbook/>`,
		"xl/worksheets/sheet1.xml": sheet,
	}))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if doc.Text != "7" {
		t.Errorf("Text = %q, want %q", doc.Text, "7")
	}
}

func TestExtractPptx(t *testing.T) {
	slide := func(body string) string {
		return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
<p:cSld><p:sp><p:txBody>` + body + `</p:txBody></p:sp></p:cSld></p:sld>`
	}

	doc, err := Extract("deck.pptx", zipBytes(t, map[string]string{
		"[Content_Types].xml":              `<Types/>`,
		"ppt/presentation.xml":             `<p:presentation/>`,
		"ppt/slides/slide1.xml":            slide(`<a:p><a:r><a:t>Welcome</a:t></a:r></a:p><a:p><a:r><a:t xml:space="preserve">Item </a:t></a:r><a:r><a:t>one</a:t></a:r></a:p>`),
		"ppt/slides/slide2.xml":            slide(`<a:p><a:r><a:t>Decide on CR_002 by Friday</a:t></a:r></a:p>`),
		"ppt/slides/slide10.xml":           slide(`<a:p><a:r><a:t>Tenth slide</a:t></a:r></a:p>`),
		"ppt/slides/_rels/slide1.xml.rels": `<Relationships><t>RELJUNK</t></Relationships>`,
		"ppt/notesSlides/notesSlide1.xml":  `<p:notes><p:txBody><a:p><a:r><a:t>Speaker note</a:t></a:r></a:p></p:txBody></p:notes>`,
	}))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	want := "Welcome\nItem one\n\nDecide on CR_002 by Friday\n\nTenth slide\n\nSpeaker note"
	if doc.Text != want {
		t.Errorf("Text = %q, want %q", doc.Text, want)
	}
	if doc.Mime != "application/vnd.openxmlformats-officedocument.presentationml.presentation" {
		t.Errorf("Mime = %q", doc.Mime)
	}
}

func TestExtractPptxWithoutSlides(t *testing.T) {
	_, err := Extract("empty.pptx", zipBytes(t, map[string]string{
		"ppt/presentation.xml": `<p:presentation/>`,
	}))
	if !errors.Is(err, ErrNoText) {
		t.Fatalf("err = %v, want ErrNoText", err)
	}
}

func TestExtractPDF(t *testing.T) {
	doc, err := Extract("minutes.pdf", minimalPDF("Approve CR_002 budget"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(doc.Text, "Approve CR_002 budget") {
		t.Errorf("Text = %q, missing expected content", doc.Text)
	}
	if doc.Mime != "application/pdf" {
		t.Errorf("Mime = %q", doc.Mime)
	}
}

func TestExtractPDFWithoutTextLayer(t *testing.T) {
	_, err := Extract("scan.pdf", minimalPDF(""))
	if !errors.Is(err, ErrNoText) {
		t.Fatalf("err = %v, want ErrNoText", err)
	}
}

func TestExtractTruncatesLongText(t *testing.T) {
	long := strings.Repeat("word ", MaxTextChars/4)
	doc, err := Extract("long.txt", []byte(long))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.HasSuffix(doc.Text, "[truncated]") {
		t.Errorf("Text not truncated: tail %q", doc.Text[len(doc.Text)-30:])
	}
	if doc.Chars != MaxTextChars+len("\n[truncated]") {
		t.Errorf("Chars = %d, want %d", doc.Chars, MaxTextChars+len("\n[truncated]"))
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// minimalPDF builds a one-page PDF with the given text drawn in Helvetica,
// including a correct xref table so the parser can resolve objects.
func minimalPDF(text string) []byte {
	stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	xrefPos := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefPos)
	return buf.Bytes()
}
