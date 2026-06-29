package parser

import (
	"bytes"
	"fmt"

	"github.com/fumiama/go-docx"
)

func ExtractDocxText(data []byte) (string, error) {
	reader := bytes.NewReader(data)
	doc, err := docx.Parse(reader, int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("parse docx: %w", err)
	}

	var text string
	for _, item := range doc.Document.Body.Items {
		switch v := item.(type) {
		case *docx.Paragraph:
			text += v.String() + "\n"
		case *docx.Table:
			text += v.String() + "\n"
		}
	}
	return text, nil
}
