package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pontus-devoteam/agent-sdk-go/pkg/tool"
	"github.com/resume-builder/backend/internal/parser"
	"github.com/resume-builder/backend/internal/store"
)

type ToolContext struct {
	NCStore     *store.NextcloudStore
	UserID      string
	ResumeID    string
	RevisionNum int
}

func (tc *ToolContext) BuildTools() []tool.Tool {
	return []tool.Tool{
		tc.extractDocxTool(),
		tc.extractPDFTool(),
		tc.analyzeResumeTool(),
		tc.getDesignThemesTool(),
		tc.extractResumeDataTool(),
		tc.generateHTMLTool(),
		tc.getSchemaTool(),
	}
}

func (tc *ToolContext) extractDocxTool() tool.Tool {
	return tool.NewFunctionTool(
		"extract_text_from_docx",
		"Download and parse a .docx file from Nextcloud by its WebDAV path. Returns the plain text content.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			path, ok := params["nextcloud_path"].(string)
			if !ok {
				return nil, fmt.Errorf("nextcloud_path is required")
			}
			data, err := tc.NCStore.DownloadFile(path)
			if err != nil {
				return nil, fmt.Errorf("download file: %w", err)
			}
			text, err := parser.ExtractDocxText(data)
			if err != nil {
				return nil, fmt.Errorf("parse docx: %w", err)
			}
			return map[string]interface{}{"extracted_text": text, "path": path}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"nextcloud_path": map[string]interface{}{"type": "string", "description": "The Nextcloud WebDAV path to the .docx file"},
		},
		"required": []string{"nextcloud_path"},
	})
}

func (tc *ToolContext) extractPDFTool() tool.Tool {
	return tool.NewFunctionTool(
		"extract_text_from_pdf",
		"Download and parse a .pdf file from Nextcloud by its WebDAV path. Returns the plain text content.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			path, ok := params["nextcloud_path"].(string)
			if !ok {
				return nil, fmt.Errorf("nextcloud_path is required")
			}
			data, err := tc.NCStore.DownloadFile(path)
			if err != nil {
				return nil, fmt.Errorf("download file: %w", err)
			}
			text, err := parser.ExtractPDFText(data)
			if err != nil {
				return nil, fmt.Errorf("parse pdf: %w", err)
			}
			return map[string]interface{}{"extracted_text": text, "path": path}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"nextcloud_path": map[string]interface{}{"type": "string", "description": "The Nextcloud WebDAV path to the .pdf file"},
		},
		"required": []string{"nextcloud_path"},
	})
}

func (tc *ToolContext) analyzeResumeTool() tool.Tool {
	return tool.NewFunctionTool(
		"analyze_resume_content",
		"Analyze raw resume text and structure it into the resume schema.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			text, _ := params["raw_text"].(string)
			return map[string]interface{}{"message": "Structure this text into the resume schema", "raw_text": text}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"raw_text": map[string]interface{}{"type": "string", "description": "The raw extracted text from the uploaded resume file"},
		},
		"required": []string{"raw_text"},
	})
}

func (tc *ToolContext) getDesignThemesTool() tool.Tool {
	return tool.NewFunctionTool(
		"get_design_themes",
		"Get all available creative design themes for resume HTML generation.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			themes := []map[string]string{
				{"name": "split", "description": "Two-column layout with colored sidebar for contact/skills and main content area for experience. Professional and distinctive."},
				{"name": "minimal", "description": "Clean centered single-column layout with generous whitespace, thin separators, and restrained color accents. Elegant and modern."},
				{"name": "bold", "description": "Strong gradient header, bold section headers with accent colors, confident typography. Makes a statement."},
				{"name": "timeline", "description": "Vertical timeline for experience section with dot markers and connecting lines. Visual storytelling for career progression."},
				{"name": "creative", "description": "Asymmetric layout with overlapping elements, geometric shapes, unique color combinations. For design/creative roles."},
				{"name": "corporate", "description": "Traditional layout with navy/charcoal palette, clear hierarchy, conservative fonts. For finance, law, consulting."},
				{"name": "tech", "description": "Dark mode or clean white with neon/cyan accents, monospace touches, terminal-inspired elements. For engineering roles."},
			}
			return map[string]interface{}{"themes": themes}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{},
	})
}

func (tc *ToolContext) extractResumeDataTool() tool.Tool {
	return tool.NewFunctionTool(
		"extract_resume_data",
		"Call this to begin extracting structured resume data from the raw text. After calling this, the LLM must produce the complete structured JSON with all sections and details.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			rawText, ok := params["raw_text"].(string)
			if !ok || rawText == "" {
				return nil, fmt.Errorf("raw_text is required")
			}

			log.Printf("tool extract_resume_data: received %d chars", len(rawText))
			log.Printf("tool extract_resume_data: first 300 chars: %.300s", rawText)

			return map[string]interface{}{
				"status":       "ready",
				"char_count":   len(rawText),
				"raw_text_loaded": true,
				"instruction": "You have the raw text. Now output a COMPLETE structured JSON with ALL sections and EVERY detail. Do NOT omit anything. Include: name, title, email, phone, location, linkedin, website, summary (preserve original wording), and ALL sections (experience, education, skills, certifications, projects, languages). For each section item, include every date, company, description line, and bullet point exactly as in the source. Return the structured JSON, then proceed to write the HTML.",
			}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"raw_text": map[string]interface{}{
				"type":        "string",
				"description": "The complete raw resume text to structure. Must be the full text, not a summary.",
			},
		},
		"required": []string{"raw_text"},
	})
}

func (tc *ToolContext) generateHTMLTool() tool.Tool {
	return tool.NewFunctionTool(
		"generate_resume_html",
		"Store the complete HTML resume document. The HTML must be a full self-contained document with inline CSS that renders a beautiful resume.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			html, ok := params["html"].(string)
			if !ok || html == "" {
				return nil, fmt.Errorf("html is required")
			}

			// Ensure it has proper HTML structure
			if !strings.Contains(html, "<!DOCTYPE") && !strings.Contains(html, "<html") {
				log.Printf("tool generate_html: WARNING - HTML lacks doctype/html tags, wrapping")
			}

			tc.RevisionNum++
			key := fmt.Sprintf("html/%s/%s/v%d.html", tc.UserID, tc.ResumeID, tc.RevisionNum)

			log.Printf("tool generate_html: storing HTML, length=%d key=%s", len(html), key)
			store.PutHTML(key, []byte(html))

			if tc.NCStore != nil {
				_ = tc.NCStore.UploadFile(key, []byte(html))
			}

			// Also cache the HTML directly by resume ID for immediate retrieval
			store.PutHTML(tc.ResumeID, []byte(html))

			return map[string]interface{}{
				"html_key":     key,
				"revision_num": tc.RevisionNum,
				"size_bytes":   len(html),
				"generated_at": time.Now().Format(time.RFC3339),
			}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"html": map[string]interface{}{"type": "string", "description": "Complete HTML document with inline CSS that renders the resume"},
		},
		"required": []string{"html"},
	})
}

func (tc *ToolContext) getSchemaTool() tool.Tool {
	return tool.NewFunctionTool(
		"get_resume_schema",
		"Get the expected JSON schema for structured resume data.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			schema := map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":     map[string]string{"type": "string", "description": "Full name"},
					"title":    map[string]string{"type": "string", "description": "Professional title / headline"},
					"email":    map[string]string{"type": "string", "description": "Email address"},
					"phone":    map[string]string{"type": "string", "description": "Phone number"},
					"location": map[string]string{"type": "string", "description": "City, State"},
					"linkedin": map[string]string{"type": "string", "description": "LinkedIn profile URL"},
					"website":  map[string]string{"type": "string", "description": "Personal website or portfolio URL"},
					"summary":  map[string]string{"type": "string", "description": "2-3 sentence professional summary"},
					"sections": map[string]interface{}{
						"type": "array", "description": "Resume sections (Experience, Education, Skills, etc.)",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"title": map[string]string{"type": "string"},
								"items": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"title": map[string]string{"type": "string"}, "subtitle": map[string]string{"type": "string"},
											"date": map[string]string{"type": "string"}, "description": map[string]string{"type": "string"},
											"bullets": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
										},
									},
								},
							},
						},
					},
				},
				"required": []string{"name", "sections"},
			}
			return map[string]interface{}{"schema": schema}, nil
		},
	).WithSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{}})
}

func (tc *ToolContext) Deserialize() string {
	return fmt.Sprintf(`{"revisionNum":%d}`, tc.RevisionNum)
}

func (tc *ToolContext) Serialize(data string) error {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return err
	}
	if n, ok := m["revisionNum"].(float64); ok {
		tc.RevisionNum = int(n)
	}
	return nil
}
