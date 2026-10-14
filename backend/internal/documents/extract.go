package documents

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxFileBytes    = 25 << 20
	MaxTextChars    = 120_000
	MaxMeetingChars = 500_000
)

var (
	ErrUnsupported = errors.New("unsupported file type")
	ErrNoText      = errors.New("no extractable text found; the file may be a scan without a text layer")
	ErrEmpty       = errors.New("file is empty")
)

type Document struct {
	Name  string
	Mime  string
	Text  string
	Chars int
}

func Extract(filename string, data []byte) (Document, error) {
	if len(data) == 0 {
		return Document{}, ErrEmpty
	}

	text, mime, err := extract(filename, data)
	if err != nil {
		return Document{}, err
	}

	text = truncate(text, MaxTextChars)
	return Document{
		Name:  filename,
		Mime:  mime,
		Text:  text,
		Chars: len([]rune(text)),
	}, nil
}

func extract(filename string, data []byte) (string, string, error) {
	switch {
	case hasPrefix(data, []byte("%PDF-")):
		text, err := extractPDF(data)
		return text, "application/pdf", err
	case hasPrefix(data, []byte{0x50, 0x4B, 0x03, 0x04}):
		return extractZip(filename, data)
	}
	return extractText(filename, data)
}

func extractText(filename string, data []byte) (string, string, error) {
	if !utf8.Valid(data) {
		return "", "", fmt.Errorf("%w: %s", ErrUnsupported, filename)
	}
	head := data
	if len(head) > 8192 {
		head = head[:8192]
	}
	for _, b := range head {
		if b == 0 {
			return "", "", fmt.Errorf("%w: %s", ErrUnsupported, filename)
		}
	}

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return "", "", ErrNoText
	}

	mime := "text/plain"
	switch strings.ToLower(ext(filename)) {
	case ".csv":
		mime = "text/csv"
	case ".md", ".markdown":
		mime = "text/markdown"
	}
	return text, mime, nil
}

func truncate(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "\n[truncated]"
}

func ext(filename string) string {
	if i := strings.LastIndexByte(filename, '.'); i >= 0 {
		return filename[i:]
	}
	return ""
}

func hasPrefix(data, prefix []byte) bool {
	if len(data) < len(prefix) {
		return false
	}
	for i := range prefix {
		if data[i] != prefix[i] {
			return false
		}
	}
	return true
}
