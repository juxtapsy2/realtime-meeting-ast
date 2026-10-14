package documents

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

func extractPDF(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to read pdf: %w", err)
	}

	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		b.WriteString(text)
		b.WriteString("\n")
	}

	if strings.TrimSpace(b.String()) == "" {
		return "", ErrNoText
	}
	return b.String(), nil
}
