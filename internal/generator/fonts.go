package generator

import (
	"log"
	"os"

	"github.com/jung-kurt/gofpdf"
)

var FontDir = "fonts"

func LoadFonts(pdf *gofpdf.Fpdf) {
	if err := os.MkdirAll(FontDir, 0755); err != nil {
		log.Printf("warning: cannot create font dir: %v", err)
		return
	}

	// gofpdf supports UTF-8 through its own font sub-setting
	// Helvetica (built-in) is good enough for Latin text in resumes
	// Custom fonts can be added:
	//
	// pdf.AddUTF8Font("Roboto", "", FontDir+"/Roboto-Regular.ttf")
	// pdf.AddUTF8Font("Roboto", "B", FontDir+"/Roboto-Bold.ttf")
	// pdf.AddUTF8Font("Lato", "", FontDir+"/Lato-Regular.ttf")
}

func ReadFontData(name string) ([]byte, error) {
	return os.ReadFile(FontDir + "/" + name)
}
