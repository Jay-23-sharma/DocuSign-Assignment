package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"signing-app/backend/internal/auth"
	"signing-app/backend/internal/documents"
)

const titleBytes = 1024

type uploadError struct {
	status  int
	message string
}

func (e uploadError) Error() string  { return e.message }
func invalid(message string) error   { return uploadError{400, message} }
func oversized(message string) error { return uploadError{413, message} }

func uploadDocument(store documents.Store) senderHandler {
	return func(w http.ResponseWriter, r *http.Request, sender auth.Sender) {
		limitBytes := store.MaxBytes + documents.Overhead
		if r.ContentLength > limitBytes {
			registrationError(w, 413, "Upload exceeds the request size limit.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limitBytes)
		draft, err := receiveDocument(r, store, sender.ID)
		if err != nil {
			var limit *http.MaxBytesError
			var client uploadError
			switch {
			case errors.As(err, &limit):
				registrationError(w, 413, "Upload exceeds the request size limit.")
			case errors.As(err, &client):
				registrationError(w, client.status, client.message)
			default:
				registrationError(w, 500, "Could not save the document.")
			}
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(draft)
	}
}

func multipartError(err error) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return err
	}
	return invalid("Invalid multipart upload.")
}

func receiveDocument(r *http.Request, store documents.Store, owner int64) (documents.Draft, error) {
	var none documents.Draft
	reader, err := r.MultipartReader()
	if err != nil {
		return none, invalid("Use multipart/form-data with one PDF.")
	}
	temp, err := os.CreateTemp(store.Dir, ".upload-*")
	if err != nil {
		return none, err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	hash := sha256.New()
	var file documents.Original
	var title string
	var hasTitle bool
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return none, multipartError(err)
		}
		switch part.FormName() {
		case "file":
			if file.Filename != "" || part.FileName() == "" {
				return none, invalid("Choose exactly one PDF file.")
			}
			file.Filename = path.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
			if !utf8.ValidString(file.Filename) || strings.ContainsRune(file.Filename, 0) {
				return none, invalid("Invalid filename.")
			}
			file.Size, err = io.Copy(io.MultiWriter(temp, hash), io.LimitReader(part, store.MaxBytes+1))
			if err != nil {
				var fsErr *os.PathError
				if errors.As(err, &fsErr) {
					return none, err
				}
				return none, multipartError(err)
			}
			if file.Size > store.MaxBytes {
				return none, oversized("PDF exceeds the file size limit.")
			}
		case "title":
			if hasTitle || part.FileName() != "" {
				return none, invalid("Provide a single text title.")
			}
			hasTitle = true
			data, err := io.ReadAll(io.LimitReader(part, titleBytes+1))
			if err != nil {
				return none, multipartError(err)
			}
			if len(data) > titleBytes {
				return none, oversized("Title exceeds 1024 bytes.")
			}
			if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
				return none, invalid("Invalid title.")
			}
			title = strings.TrimSpace(string(data))
		default:
			return none, invalid("Unexpected upload field.")
		}
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return none, multipartError(err)
	}
	if file.Size == 0 {
		return none, invalid("Choose one nonempty PDF file.")
	}
	if title == "" {
		title = strings.TrimSpace(file.Filename)
		if strings.HasSuffix(strings.ToLower(title), ".pdf") {
			title = strings.TrimSpace(title[:len(title)-4])
		}
		if title == "" {
			title = "Untitled document"
		}
		if len(title) > titleBytes {
			return none, oversized("Title exceeds 1024 bytes.")
		}
	}
	if _, err = temp.Seek(0, io.SeekStart); err != nil {
		return none, err
	}
	file.Pages, err = validatePDF(r.Context(), temp)
	if err != nil {
		return none, err
	}
	if err = temp.Sync(); err != nil {
		return none, err
	}
	return store.Save(r.Context(), owner, title, temp.Name(), file, hash.Sum(nil))
}

func validatePDF(ctx context.Context, file *os.File) (int, error) {
	conf := model.NewStatelessConfiguration()
	conf.Offline = true
	// Browser-generated PDFs may use newer font dictionary entries than their declared version.
	conf.ValidationMode = model.ValidationRelaxed
	pdf, err := api.ReadAndValidate(ctx, file, conf)
	if errors.Is(err, pdfcpu.ErrWrongPassword) || (err == nil && pdf.Encrypt != nil) {
		return 0, invalid("Password-protected PDFs are not supported.")
	}
	if err != nil || pdf.PageCount < 1 {
		return 0, invalid("Invalid PDF. Choose a readable PDF with at least one page.")
	}
	return pdf.PageCount, nil
}
