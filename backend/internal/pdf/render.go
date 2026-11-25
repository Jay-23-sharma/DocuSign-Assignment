package pdf

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
)

//go:embed render.py
var renderer string

type Mark struct {
	Page    int     `json:"page"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Type    string  `json:"type"`
	Text    *string `json:"text"`
	Checked *bool   `json:"checked"`
	Image   string  `json:"image"`
}

// The worker reads only private staged files. Answers travel over stdin, never argv.
func Render(ctx context.Context, dir string, pages int, fields []Mark) error {
	data, e := json.Marshal(struct {
		Pages  int    `json:"pages"`
		Fields []Mark `json:"fields"`
	}{pages, fields})
	if e != nil {
		return e
	}
	python := os.Getenv("PDF_RENDER_PYTHON")
	if python == "" {
		python = "python3"
	}
	cmd := exec.CommandContext(ctx, python, "-c", renderer)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(data)
	return cmd.Run()
}
