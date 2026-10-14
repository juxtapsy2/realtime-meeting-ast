package documents

import (
	"archive/zip"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// partNumber matches the trailing index of OOXML part names (slide2.xml,
// notesSlide10.xml) so slides sort naturally instead of lexicographically.
var partNumber = regexp.MustCompile(`(\d+)\.xml$`)

func partLess(a, b string) bool {
	na, aok := partIndex(a)
	nb, bok := partIndex(b)
	if aok && bok && na != nb {
		return na < nb
	}
	return a < b
}

func partIndex(name string) (int, bool) {
	m := partNumber.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// partNames lists the XML parts under dir (excluding .rels), in natural order.
func partNames(zr *zip.Reader, dir string) []string {
	var names []string
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, dir) ||
			strings.HasSuffix(f.Name, ".rels") ||
			!strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		names = append(names, f.Name)
	}
	sort.Slice(names, func(i, j int) bool { return partLess(names[i], names[j]) })
	return names
}

// extractPptx returns every slide in slide order, followed by the speaker
// notes. The shared paragraph decoder handles a:p/a:t runs; relationship
// parts are skipped, and an archive with no readable text maps to ErrNoText.
func extractPptx(zr *zip.Reader) (string, error) {
	names := append(partNames(zr, "ppt/slides/"), partNames(zr, "ppt/notesSlides/")...)
	if len(names) == 0 {
		return "", ErrNoText
	}

	byName := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		byName[f.Name] = f
	}

	var b strings.Builder
	for _, name := range names {
		f := byName[name]
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		text, err := decodeParagraphs(rc, name)
		rc.Close()
		if err != nil {
			return "", err
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(text)
	}

	out := strings.TrimRight(b.String(), "\n")
	if strings.TrimSpace(out) == "" {
		return "", ErrNoText
	}
	return out, nil
}
