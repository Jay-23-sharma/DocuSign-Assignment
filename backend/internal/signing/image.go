package signing

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
)

const MaxImageBytes = 2 << 20

var invalidImage = Problem{422, "Choose a readable PNG or JPEG up to 2 MiB, at most 4096 pixels per side and 4 million pixels in total."}

func imageMethod(method string) bool { return method == "drawn" || method == "uploaded" }
func imageKey(g session, method, reference string) string {
	return filepath.Join(fmt.Sprint(g.doc), "signatures", fmt.Sprint(g.recipient), method, reference+".png")
}
func (s Service) imageAnswer(g session, a Answer) (*string, error) {
	if a.Method == nil || !imageMethod(*a.Method) || a.Reference == nil || !validSecret(*a.Reference) || a.Text != nil {
		return nil, invalidAnswer
	}
	key := imageKey(g, *a.Method, *a.Reference)
	f, e := os.OpenInRoot(s.Documents.Dir, key)
	if os.IsNotExist(e) {
		return nil, invalidAnswer
	}
	if e != nil {
		return nil, e
	}
	f.Close()
	return &key, nil
}
func (s Service) SaveImage(ctx context.Context, c Credentials, method string, data []byte) (ref string, err error) {
	if !imageMethod(method) || len(data) > MaxImageBytes {
		return "", invalidImage
	}
	config, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || (format != "png" && format != "jpeg") || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || config.Width*config.Height > 4000000 {
		return "", invalidImage
	}
	img, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return "", invalidImage
	}
	// Decode and re-encode before publication. Ancillary metadata and trailing bytes are discarded.
	var normalized bytes.Buffer
	if e = png.Encode(&normalized, img); e != nil {
		return "", e
	}
	err = s.withSession(ctx, c, true, func(tx pgx.Tx, g session) error {
		if !g.open() {
			return closed
		}
		ref = secret()
		path := filepath.Join(s.Documents.Dir, imageKey(g, method, ref))
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		out, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = normalized.WriteTo(out)
		closeErr := out.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			os.Remove(path)
		}
		return e
	})
	return
}
