package parser

import (
	"fmt"
	"log"
	"strings"

	"github.com/ledongthuc/pdf"
)

func ExtractPDFText(data []byte) (string, error) {
	log.Printf("pdf parser: opening %d bytes", len(data))

	doc, err := pdf.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}

	numPages := doc.NumPage()
	log.Printf("pdf parser: %d pages", numPages)

	var allText string

	for i := 1; i <= numPages; i++ {
		page := doc.Page(i)
		if page.V.IsNull() {
			log.Printf("pdf parser: page %d is null, skipping", i)
			continue
		}

		// Method 1: GetPlainText with fonts loaded from page
		text1, err := page.GetPlainText(nil)
		if err != nil {
			log.Printf("pdf parser: GetPlainText failed page %d: %v", i, err)
		}

		// Method 2: Content() text marks — extract raw text from positioned glyphs
		var text2 string
		content := page.Content()
		for _, t := range content.Text {
			text2 += t.S
		}

		// Method 3: GetTextByRow — groups text into rows
		var text3 string
		rows, err := page.GetTextByRow()
		if err != nil {
			log.Printf("pdf parser: GetTextByRow failed page %d: %v", i, err)
		} else {
			for _, row := range rows {
				for _, t := range row.Content {
					text3 += t.S + " "
				}
				text3 += "\n"
			}
		}

		// Pick the longest extraction result for this page
		pageText := text1
		if len(text3) > len(pageText) {
			pageText = text3
		}
		if len(text2) > len(pageText) {
			pageText = text2
		}

		log.Printf("pdf parser: page %d — GetPlainText=%d chars, Content=%d chars, GetTextByRow=%d chars, using=%d chars",
			i, len(text1), len(text2), len(text3), len(pageText))

		allText += pageText + "\n"
	}

	// Clean up: collapse excessive whitespace but preserve line breaks
	allText = strings.TrimSpace(allText)

	log.Printf("pdf parser: TOTAL extracted %d chars", len(allText))
	if len(allText) > 0 {
		preview := allText
		if len(preview) > 500 {
			preview = preview[:500]
		}
		log.Printf("pdf parser: preview: %s", preview)
	} else {
		log.Printf("pdf parser: WARNING — no text extracted. PDF may be scanned/image-based.")
	}

	return allText, nil
}
