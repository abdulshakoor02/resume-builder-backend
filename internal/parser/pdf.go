package parser

import (
	"fmt"
	"log"

	"github.com/razvandimescu/gopdf/pdf"
)

func ExtractPDFText(data []byte) (string, error) {
	log.Printf("pdf parser: opening %d bytes", len(data))

	doc, err := pdf.OpenBytes(data)
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}

	log.Printf("pdf parser: document opened")

	text, err := doc.Text()
	if err != nil {
		return "", fmt.Errorf("extract text: %w", err)
	}

	log.Printf("pdf parser: extracted %d chars of text", len(text))
	if len(text) > 0 {
		log.Printf("pdf parser: first 200 chars: %.200s", text)
	} else {
		log.Printf("pdf parser: WARNING - no text extracted from PDF. The PDF may be scanned/image-based.")
	}

	return text, nil
}
