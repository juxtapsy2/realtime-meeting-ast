package meetings

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/user/realtime-meeting-ast/backend/internal/documents"
	"github.com/user/realtime-meeting-ast/backend/internal/types"
)

const (
	maxUploadBytes = documents.MaxFileBytes + (1 << 20)
	maxFormMemory  = 8 << 20
	maxFilesPerReq = 5
	maxLLMChars    = 200_000
)

type DocumentsResponse struct {
	Documents []types.MeetingDocument `json:"documents"`
}

func (s *Service) UploadMeetingDocuments(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}

	if _, err := s.repo.GetByID(id); err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxFormMemory); err != nil {
		http.Error(w, "Invalid multipart form", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()

	var incoming []documents.Document
	for _, files := range r.MultipartForm.File {
		for _, fh := range files {
			if len(incoming) >= maxFilesPerReq {
				http.Error(w, fmt.Sprintf("Too many files; at most %d per request", maxFilesPerReq), http.StatusBadRequest)
				return
			}

			f, err := fh.Open()
			if err != nil {
				http.Error(w, "Failed to read uploaded file", http.StatusBadRequest)
				return
			}
			data, err := io.ReadAll(io.LimitReader(f, documents.MaxFileBytes+1))
			f.Close()
			if err != nil {
				http.Error(w, "Failed to read uploaded file", http.StatusBadRequest)
				return
			}
			if len(data) > documents.MaxFileBytes {
				http.Error(w, fmt.Sprintf("File %q is too large; the limit is %d MB", fh.Filename, documents.MaxFileBytes>>20), http.StatusBadRequest)
				return
			}

			doc, err := documents.Extract(fh.Filename, data)
			if err != nil {
				http.Error(w, fmt.Sprintf("File %q: %v", fh.Filename, err), http.StatusBadRequest)
				return
			}
			incoming = append(incoming, doc)
		}
	}

	if len(incoming) == 0 {
		http.Error(w, "No files uploaded", http.StatusBadRequest)
		return
	}

	docs, err := s.upsertDocuments(id, incoming)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DocumentsResponse{Documents: docs})
}

func (s *Service) ClearMeetingDocuments(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid meeting ID", http.StatusBadRequest)
		return
	}

	if _, err := s.repo.GetByID(id); err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	if err := s.repo.SaveDocuments(id, nil); err != nil {
		http.Error(w, "Failed to clear documents", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) upsertDocuments(meetingID int, incoming []documents.Document) ([]types.MeetingDocument, error) {
	stored, err := s.repo.GetStoredDocuments(meetingID)
	if err != nil {
		return nil, fmt.Errorf("Failed to load existing documents")
	}

	byName := map[string]storedDocument{}
	order := make([]string, 0, len(stored)+len(incoming))
	totalChars := 0
	for _, d := range stored {
		byName[d.Name] = d
		order = append(order, d.Name)
		totalChars += d.Chars
	}

	now := time.Now().UTC()
	for _, in := range incoming {
		if prev, ok := byName[in.Name]; ok {
			totalChars -= prev.Chars
		} else {
			order = append(order, in.Name)
		}
		byName[in.Name] = storedDocument{
			Name:       in.Name,
			Mime:       in.Mime,
			Chars:      in.Chars,
			UploadedAt: now,
			Text:       in.Text,
		}
		totalChars += in.Chars
	}

	if totalChars > documents.MaxMeetingChars {
		return nil, fmt.Errorf("Documents total %d characters; the limit is %d", totalChars, documents.MaxMeetingChars)
	}

	merged := make([]storedDocument, 0, len(order))
	for _, name := range order {
		merged = append(merged, byName[name])
	}
	if err := s.repo.SaveDocuments(meetingID, merged); err != nil {
		return nil, fmt.Errorf("Failed to save documents")
	}

	docs := make([]types.MeetingDocument, 0, len(merged))
	for _, d := range merged {
		docs = append(docs, types.MeetingDocument{
			Name:       d.Name,
			Mime:       d.Mime,
			Chars:      d.Chars,
			UploadedAt: d.UploadedAt,
		})
	}
	return docs, nil
}

func documentsText(stored []storedDocument) string {
	var b strings.Builder
	remaining := maxLLMChars
	for _, d := range stored {
		if remaining <= 0 {
			break
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
			remaining -= 2
		}
		b.WriteString("[file: ")
		b.WriteString(d.Name)
		b.WriteString("]\n")
		remaining -= len(d.Name) + 9

		text := d.Text
		if runes := []rune(text); len(runes) > remaining {
			text = string(runes[:remaining]) + "\n[truncated]"
		}
		b.WriteString(text)
		remaining -= len([]rune(text))
	}
	return b.String()
}
