package generator

import (
	"bytes"
	"fmt"

	"github.com/jung-kurt/gofpdf"
)

type Section struct {
	Title  string
	Items  []SectionItem
}

type SectionItem struct {
	Title       string
	Subtitle    string
	Date        string
	Description string
	Bullets     []string
}

type ResumeData struct {
	Name        string
	Title       string
	Email       string
	Phone       string
	Location    string
	LinkedIn    string
	Website     string
	Summary     string
	Sections    []Section
	Template    string
}

type Generator struct {
	fontsLoaded bool
}

func NewGenerator() *Generator {
	return &Generator{}
}

func (g *Generator) GeneratePDF(data *ResumeData) ([]byte, error) {
	switch data.Template {
	case "modern":
		return g.generateModern(data)
	case "minimal":
		return g.generateMinimal(data)
	default:
		return g.generateClassic(data)
	}
}

func (g *Generator) generateClassic(data *ResumeData) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	g.renderHeader(pdf, data)
	g.renderSummary(pdf, data)
	g.renderSections(pdf, data)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output: %w", err)
	}
	return buf.Bytes(), nil
}

func (g *Generator) generateModern(data *ResumeData) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)

	pdf.AddPage()

	// Modern: colored header band
	pdf.SetFillColor(41, 65, 119)
	pdf.Rect(0, 0, 210, 45, "F")
	pdf.SetTextColor(255, 255, 255)

	pdf.SetFont("Helvetica", "B", 24)
	pdf.SetY(10)
	pdf.CellFormat(190, 12, data.Name, "", 1, "C", false, 0, "")

	pdf.SetFont("Helvetica", "", 12)
	pdf.CellFormat(190, 8, data.Title, "", 1, "C", false, 0, "")
	pdf.CellFormat(190, 6, fmt.Sprintf("%s | %s | %s", data.Email, data.Phone, data.Location), "", 1, "C", false, 0, "")

	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(8)

	g.renderSummary(pdf, data)
	g.renderSections(pdf, data)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output: %w", err)
	}
	return buf.Bytes(), nil
}

func (g *Generator) generateMinimal(data *ResumeData) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(25, 25, 25)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddPage()

	// Minimal: clean, thin lines
	pdf.SetFont("Helvetica", "B", 22)
	pdf.CellFormat(0, 10, data.Name, "", 1, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 11)
	pdf.SetTextColor(80, 80, 80)
	pdf.CellFormat(0, 6, data.Title, "", 1, "L", false, 0, "")

	pdf.SetTextColor(0, 0, 0)
	contactLine := fmt.Sprintf("%s  •  %s  •  %s", data.Email, data.Phone, data.Location)
	pdf.SetFont("Helvetica", "", 9)
	pdf.CellFormat(0, 6, contactLine, "", 1, "L", false, 0, "")

	// Thin separator line
	pdf.Ln(2)
	pdf.SetDrawColor(200, 200, 200)
	pdf.Line(25, pdf.GetY(), 185, pdf.GetY())
	pdf.Ln(4)

	g.renderSummary(pdf, data)
	g.renderSections(pdf, data)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf output: %w", err)
	}
	return buf.Bytes(), nil
}

func (g *Generator) renderHeader(pdf *gofpdf.Fpdf, data *ResumeData) {
	pdf.SetFont("Helvetica", "B", 24)
	pdf.CellFormat(0, 10, data.Name, "", 1, "C", false, 0, "")

	pdf.SetFont("Helvetica", "", 12)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(0, 6, data.Title, "", 1, "C", false, 0, "")

	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Helvetica", "", 9)
	contact := ""
	if data.Email != "" {
		contact += data.Email
	}
	if data.Phone != "" {
		if contact != "" {
			contact += " | "
		}
		contact += data.Phone
	}
	if data.Location != "" {
		if contact != "" {
			contact += " | "
		}
		contact += data.Location
	}
	pdf.CellFormat(0, 6, contact, "", 1, "C", false, 0, "")

	// Separator line
	pdf.Ln(2)
	pdf.SetDrawColor(150, 150, 150)
	pdf.Line(20, pdf.GetY(), 190, pdf.GetY())
	pdf.Ln(4)
}

func (g *Generator) renderSummary(pdf *gofpdf.Fpdf, data *ResumeData) {
	if data.Summary == "" {
		return
	}

	pdf.SetFont("Helvetica", "B", 12)
	pdf.SetTextColor(41, 65, 119)
	pdf.CellFormat(0, 7, "PROFESSIONAL SUMMARY", "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.SetDrawColor(41, 65, 119)
	pdf.Line(20, pdf.GetY(), 60, pdf.GetY())
	pdf.Ln(3)

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(50, 50, 50)
	pdf.MultiCell(0, 5, data.Summary, "", "L", false)
	pdf.Ln(4)
}

func (g *Generator) renderSections(pdf *gofpdf.Fpdf, data *ResumeData) {
	for _, section := range data.Sections {
		g.renderSection(pdf, section)
	}
}

func (g *Generator) renderSection(pdf *gofpdf.Fpdf, section Section) {
	pdf.SetFont("Helvetica", "B", 12)
	pdf.SetTextColor(41, 65, 119)
	pdf.CellFormat(0, 7, section.Title, "", 1, "L", false, 0, "")
	pdf.SetDrawColor(41, 65, 119)
	pdf.Line(20, pdf.GetY(), 60, pdf.GetY())
	pdf.Ln(3)

	for _, item := range section.Items {
		pdf.SetFont("Helvetica", "B", 11)
		pdf.SetTextColor(0, 0, 0)
		pdf.CellFormat(0, 6, item.Title, "", 1, "L", false, 0, "")

		if item.Subtitle != "" || item.Date != "" {
			pdf.SetFont("Helvetica", "I", 10)
			pdf.SetTextColor(80, 80, 80)
			line := item.Subtitle
			if item.Date != "" {
				if line != "" {
					line += " | "
				}
				line += item.Date
			}
			pdf.CellFormat(0, 5, line, "", 1, "L", false, 0, "")
		}

		if item.Description != "" {
			pdf.SetFont("Helvetica", "", 10)
			pdf.SetTextColor(60, 60, 60)
			pdf.MultiCell(0, 5, item.Description, "", "L", false)
		}

		for _, bullet := range item.Bullets {
			pdf.SetFont("Helvetica", "", 10)
			pdf.SetTextColor(60, 60, 60)
			pdf.CellFormat(5, 5, "•", "", 0, "L", false, 0, "")
			pdf.MultiCell(0, 5, bullet, "", "L", false)
		}

		if len(item.Bullets) > 0 || item.Description != "" {
			pdf.Ln(2)
		}
	}
	pdf.Ln(2)
}
