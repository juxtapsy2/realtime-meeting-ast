package documents

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

func extractZip(filename string, data []byte) (string, string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", "", fmt.Errorf("%w: %s (%v)", ErrUnsupported, filename, err)
	}

	for _, f := range zr.File {
		switch f.Name {
		case "word/document.xml":
			text, err := extractDocx(zr, f)
			return text, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", err
		case "xl/workbook.xml", "xl/sharedStrings.xml":
			text, err := extractXlsx(zr)
			return text, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", err
		case "ppt/presentation.xml":
			text, err := extractPptx(zr)
			return text, "application/vnd.openxmlformats-officedocument.presentationml.presentation", err
		}
	}
	return "", "", fmt.Errorf("%w: %s", ErrUnsupported, filename)
}

func extractDocx(zr *zip.Reader, f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	return decodeParagraphs(rc, f.Name)
}

// decodeParagraphs streams text runs (<t>) out of an OOXML body, joining them
// per paragraph (<p>). Word documents and PowerPoint slides/notes share this
// shape (w:p/w:t vs a:p/a:t), so both use this decoder.
func decodeParagraphs(r io.Reader, source string) (string, error) {
	var b strings.Builder
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to parse %s: %w", source, err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				var el struct {
					Text string `xml:",chardata"`
				}
				if err := dec.DecodeElement(&el, &t); err != nil {
					return "", err
				}
				b.WriteString(el.Text)
			case "br", "cr":
				b.WriteString("\n")
			case "tab":
				b.WriteString("\t")
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				b.WriteString("\n")
			}
		}
	}

	text := b.String()
	if trimEmpty := strings.TrimSpace(text); trimEmpty == "" {
		return "", ErrNoText
	}
	return text, nil
}
