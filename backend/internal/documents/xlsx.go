package documents

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

type xlsxCell struct {
	Type string `xml:"t,attr"`
	V    string `xml:"v"`
	IS   struct {
		T []string `xml:"t"`
	} `xml:"is"`
}

func extractXlsx(zr *zip.Reader) (string, error) {
	shared, err := readSharedStrings(zr)
	if err != nil {
		return "", err
	}

	var sheets []*zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/") && !strings.HasSuffix(f.Name, ".rels") {
			sheets = append(sheets, f)
		}
	}
	if len(sheets) == 0 {
		return "", ErrNoText
	}
	sort.Slice(sheets, func(i, j int) bool { return sheets[i].Name < sheets[j].Name })

	var b strings.Builder
	for i, f := range sheets {
		if i > 0 {
			b.WriteString("\n")
		}
		rows, err := readSheet(f, shared)
		if err != nil {
			return "", err
		}
		b.WriteString(rows)
	}

	text := b.String()
	if strings.TrimSpace(text) == "" {
		return "", ErrNoText
	}
	return text, nil
}

func readSharedStrings(zr *zip.Reader) ([]string, error) {
	f, err := zr.Open("xl/sharedStrings.xml")
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	var shared []string
	dec := xml.NewDecoder(f)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse sharedStrings.xml: %w", err)
		}
		si, ok := tok.(xml.StartElement)
		if !ok || si.Name.Local != "si" {
			continue
		}

		var el struct {
			T []string `xml:"t"`
		}
		if err := dec.DecodeElement(&el, &si); err != nil {
			return nil, fmt.Errorf("failed to parse shared string: %w", err)
		}
		shared = append(shared, strings.Join(el.T, ""))
	}
	return shared, nil
}

func readSheet(f *zip.File, shared []string) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	var b strings.Builder
	dec := xml.NewDecoder(rc)
	inSheetData := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to parse %s: %w", f.Name, err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "sheetData":
				inSheetData = true
			case "row":
				if inSheetData {
					if b.Len() > 0 {
						b.WriteString("\n")
					}
					if err := decodeRow(dec, &t, shared, &b); err != nil {
						return "", err
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "sheetData" {
				inSheetData = false
			}
		}
	}
	return b.String(), nil
}

func decodeRow(dec *xml.Decoder, row *xml.StartElement, shared []string, b *strings.Builder) error {
	first := true
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("failed to parse row: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "c" {
				if err := dec.Skip(); err != nil {
					return err
				}
				continue
			}
			var cell xlsxCell
			if err := dec.DecodeElement(&cell, &t); err != nil {
				return err
			}
			value := cellValue(cell, shared)
			if !first {
				b.WriteString("\t")
			}
			b.WriteString(value)
			first = false
		case xml.EndElement:
			if t.Name.Local == "row" {
				return nil
			}
		}
	}
}

func cellValue(c xlsxCell, shared []string) string {
	switch c.Type {
	case "s":
		i := 0
		if _, err := fmt.Sscanf(c.V, "%d", &i); err == nil && i >= 0 && i < len(shared) {
			return shared[i]
		}
		return ""
	case "inlineStr":
		return strings.Join(c.IS.T, "")
	case "b", "e":
		return ""
	default:
		return c.V
	}
}
