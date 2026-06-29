package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/pontus-devoteam/agent-sdk-go/pkg/tool"
	"github.com/resume-builder/backend/internal/generator"
	"github.com/resume-builder/backend/internal/parser"
	"github.com/resume-builder/backend/internal/store"
)

type ToolContext struct {
	NCStore      *store.NextcloudStore
	Generator    *generator.Generator
	UserID       string
	ResumeID     string
	RevisionNum  int
}

func (tc *ToolContext) BuildTools() []tool.Tool {
	return []tool.Tool{
		tc.extractDocxTool(),
		tc.extractPDFTool(),
		tc.analyzeResumeTool(),
		tc.getTemplatesTool(),
		tc.generatePDFTool(),
		tc.validatePDFTool(),
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
			return map[string]interface{}{
				"extracted_text": text,
				"path":           path,
			}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"nextcloud_path": map[string]interface{}{
				"type":        "string",
				"description": "The Nextcloud WebDAV path to the .docx file (e.g. uploads/user123/file.docx)",
			},
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
			return map[string]interface{}{
				"extracted_text": text,
				"path":           path,
			}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"nextcloud_path": map[string]interface{}{
				"type":        "string",
				"description": "The Nextcloud WebDAV path to the .pdf file",
			},
		},
		"required": []string{"nextcloud_path"},
	})
}

func (tc *ToolContext) analyzeResumeTool() tool.Tool {
	return tool.NewFunctionTool(
		"analyze_resume_content",
		"Analyze raw resume text and structure it into the resume schema. Returns structured sections with improved wording.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			text, ok := params["raw_text"].(string)
			if !ok {
				return nil, fmt.Errorf("raw_text is required")
			}
			_ = text
			return map[string]interface{}{
				"message": "Use the LLM to structure this text into the resume schema. The raw_text has been received.",
				"raw_text": text,
			}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"raw_text": map[string]interface{}{
				"type":        "string",
				"description": "The raw extracted text from the uploaded resume file",
			},
		},
		"required": []string{"raw_text"},
	})
}

func (tc *ToolContext) getTemplatesTool() tool.Tool {
	return tool.NewFunctionTool(
		"get_layout_templates",
		"Get all available resume layout templates with their descriptions.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			templates := generator.GetTemplates()
			return map[string]interface{}{
				"templates": templates,
			}, nil
		},
	).WithSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	})
}

func (tc *ToolContext) generatePDFTool() tool.Tool {
	return tool.NewFunctionTool(
		"generate_resume_pdf",
		"Generate a PDF resume from structured resume data and a chosen template. Uploads the PDF to Nextcloud and returns the path.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			template, ok := params["template"].(string)
			if !ok || template == "" {
				template = "classic"
			}

			dataJSON, err := json.Marshal(params["resume_data"])
			if err != nil {
				return nil, fmt.Errorf("marshal resume data: %w", err)
			}

			var data generator.ResumeData
			if err := json.Unmarshal(dataJSON, &data); err != nil {
				return nil, fmt.Errorf("unmarshal resume data: %w", err)
			}
			data.Template = template

			pdfBytes, err := tc.Generator.GeneratePDF(&data)
			if err != nil {
				log.Printf("tool generate_pdf: generation failed: %v", err)
				return nil, fmt.Errorf("generate pdf: %w", err)
			}

			tc.RevisionNum++
			remotePath := fmt.Sprintf("outputs/%s/%s/v%d.pdf", tc.UserID, tc.ResumeID, tc.RevisionNum)

			log.Printf("tool generate_pdf: generated %d bytes, uploading to %s", len(pdfBytes), remotePath)

			store.PutPDF(remotePath, pdfBytes)

			if tc.NCStore != nil {
				_ = tc.NCStore.UploadFile(remotePath, pdfBytes)
			}

			return map[string]interface{}{
				"pdf_path":      remotePath,
				"revision_num":  tc.RevisionNum,
				"size_bytes":    len(pdfBytes),
				"generated_at":  time.Now().Format(time.RFC3339),
			}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"resume_data": map[string]interface{}{
				"type":        "object",
				"description": "Structured resume data matching the resume schema",
			},
			"template": map[string]interface{}{
				"type":        "string",
				"description": "Template name: classic, modern, or minimal",
			},
		},
		"required": []string{"resume_data"},
	})
}

func (tc *ToolContext) validatePDFTool() tool.Tool {
	return tool.NewFunctionTool(
		"validate_pdf_output",
		"Validate a generated PDF by checking it exists and reporting its basic properties. Returns feedback for any issues found.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			path, ok := params["pdf_path"].(string)
			if !ok {
				return nil, fmt.Errorf("pdf_path is required")
			}

			data, err := tc.NCStore.DownloadFile(path)
			if err != nil {
				return map[string]interface{}{
					"valid":   false,
					"error":   fmt.Sprintf("Cannot read PDF: %v", err),
					"issues":  []string{"PDF file not found or inaccessible"},
				}, nil
			}

			// Basic validation: check file is non-empty, starts with PDF header
			issues := []string{}
			if len(data) == 0 {
				issues = append(issues, "PDF is empty")
			}
			if len(data) < 5 || string(data[:5]) != "%PDF-" {
				issues = append(issues, "File does not appear to be a valid PDF")
			}
			if len(data) > 2*1024*1024 {
				issues = append(issues, "PDF is larger than 2MB - consider reducing content")
			}

			return map[string]interface{}{
				"valid":     len(issues) == 0,
				"size_kb":   len(data) / 1024,
				"issues":    issues,
				"path":      path,
			}, nil
		},
	).WithSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pdf_path": map[string]interface{}{
				"type":        "string",
				"description": "The Nextcloud WebDAV path to the generated PDF",
			},
		},
		"required": []string{"pdf_path"},
	})
}

func (tc *ToolContext) getSchemaTool() tool.Tool {
	return tool.NewFunctionTool(
		"get_resume_schema",
		"Get the expected JSON schema for structured resume data. Use this to understand what fields are available.",
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			schema := map[string]interface{}{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"type": "object",
				"properties": map[string]interface{}{
					"name":        map[string]string{"type": "string", "description": "Full name"},
					"title":       map[string]string{"type": "string", "description": "Professional title / headline"},
					"email":       map[string]string{"type": "string", "description": "Email address"},
					"phone":       map[string]string{"type": "string", "description": "Phone number"},
					"location":    map[string]string{"type": "string", "description": "City, State"},
					"linkedin":    map[string]string{"type": "string", "description": "LinkedIn profile URL"},
					"website":     map[string]string{"type": "string", "description": "Personal website or portfolio URL"},
					"summary":     map[string]string{"type": "string", "description": "2-3 sentence professional summary"},
					"sections": map[string]interface{}{
						"type": "array",
						"description": "Resume sections (Experience, Education, Skills, Projects, etc.)",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"title": map[string]string{"type": "string"},
								"items": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"title":       map[string]string{"type": "string"},
											"subtitle":    map[string]string{"type": "string"},
											"date":        map[string]string{"type": "string"},
											"description": map[string]string{"type": "string"},
											"bullets": map[string]interface{}{
												"type": "array",
												"items": map[string]string{"type": "string"},
											},
										},
									},
								},
							},
						},
					},
				},
				"required": []string{"name", "sections"},
			}
			return map[string]interface{}{
				"schema": schema,
			}, nil
		},
	).WithSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	})
}
