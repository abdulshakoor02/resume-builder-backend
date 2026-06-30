package parser

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

type visionRequest struct {
	Model     string          `json:"model"`
	Messages  []visionMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens"`
}

type visionMessage struct {
	Role    string          `json:"role"`
	Content []visionContent `json:"content"`
}

type visionContent struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *visionImageURL `json:"image_url,omitempty"`
}

type visionImageURL struct {
	URL string `json:"url"`
}

type visionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// pdftoImages converts a PDF to PNG images using available system tools.
// Tries pdftoppm (poppler), then magick (ImageMagick), then sips (macOS).
func pdftoImages(pdfData []byte) ([][]byte, error) {
	tmpDir, err := os.MkdirTemp("", "pdf-images-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	pdfPath := filepath.Join(tmpDir, "input.pdf")
	if err := os.WriteFile(pdfPath, pdfData, 0644); err != nil {
		return nil, err
	}

	// Try pdftoppm (poppler-utils)
	if _, err := exec.LookPath("pdftoppm"); err == nil {
		outPrefix := filepath.Join(tmpDir, "page")
		cmd := exec.Command("pdftoppm", "-png", "-r", "200", pdfPath, outPrefix)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("pdftoppm failed: %v, output: %s", err, string(output))
		} else {
			return readImages(tmpDir, "page-*.png")
		}
	}

	// Try magick (ImageMagick)
	if _, err := exec.LookPath("magick"); err == nil {
		outPrefix := filepath.Join(tmpDir, "page")
		cmd := exec.Command("magick", "-density", "200", pdfPath, outPrefix+".png")
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("magick failed: %v, output: %s", err, string(output))
		} else {
			return readImages(tmpDir, "page-*.png")
		}
	}

	// Try convert (ImageMagick v6)
	if _, err := exec.LookPath("convert"); err == nil {
		outPrefix := filepath.Join(tmpDir, "page")
		cmd := exec.Command("convert", "-density", "200", pdfPath, outPrefix+".png")
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("convert failed: %v, output: %s", err, string(output))
		} else {
			return readImages(tmpDir, "page-*.png")
		}
	}

// macOS fallback: qlmanage thumbnail renders only page 1.
	// For multi-page PDFs, qlmanage won't help, so only use it as last resort
	// and warn if we may be missing pages.
	if _, err := exec.LookPath("qlmanage"); err == nil {
		cmd := exec.Command("qlmanage", "-t", "-s", "2000", "-o", tmpDir, pdfPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("qlmanage failed: %v, output: %s", err, string(output))
		} else {
			// qlmanage outputs as input.pdf.png
			matches, _ := filepath.Glob(filepath.Join(tmpDir, "*.png"))
			if len(matches) > 0 {
				images, err := readImages(tmpDir, "*.png")
				if err != nil {
					return nil, err
				}
				log.Printf("qlmanage: generated %d image(s) — note: only page 1 of multi-page PDFs is rendered", len(images))
				return images, nil
			}
		}
	}

	return nil, fmt.Errorf("no PDF-to-image converter found (install poppler-utils: brew install poppler)")
}

func readImages(dir, pattern string) ([][]byte, error) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	var images [][]byte
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			log.Printf("readImages: skipping unreadable file %s: %v", m, err)
			continue
		}
		images = append(images, data)
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("no images generated")
	}
	return images, nil
}

func ExtractPDFTextWithVision(data []byte) (string, error) {
	apiKey := os.Getenv("LLM_API_KEY")
	model := os.Getenv("LLM_MODEL")
	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if apiKey == "" {
		return "", fmt.Errorf("LLM_API_KEY not set for vision extraction")
	}

	log.Printf("vision extraction: converting PDF to images...")

	images, err := pdftoImages(data)
	if err != nil {
		return "", fmt.Errorf("convert PDF to images: %w", err)
	}

	log.Printf("vision extraction: %d images generated, sending to %s model %s", len(images), baseURL, model)

	// Configurable max tokens for vision extraction (default 16000, up to 128000).
	// Resumes with dense content need a large output window.
	maxTokens := 16000
	if v := os.Getenv("LLM_VISION_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTokens = n
		}
	}

	// Process pages one at a time for multi-page PDFs to avoid model truncation.
	// Each page gets its own API call, then results are concatenated.
	var allText string
	for pageIdx, img := range images {
		b64 := base64.StdEncoding.EncodeToString(img)
		log.Printf("vision extraction: sending page %d/%d, image = %d bytes (%d base64 chars)", pageIdx+1, len(images), len(img), len(b64))

		content := []visionContent{
			{
				Type: "text",
				Text: fmt.Sprintf(`You are performing OCR text extraction on page %d of %d from a resume/CV PDF.

Extract EVERY piece of text visible on this page. Output the raw text exactly as it appears, preserving:
- Every name, email, phone, location, LinkedIn URL
- Every section heading (Summary, Experience, Education, Skills, Certifications, Projects, etc.)
- Every job title, company name, and date range
- Every bullet point under each job — copy them verbatim
- Every skill, certification, education entry, and project detail

Rules:
1. Do NOT summarize, paraphrase, abbreviate, or skip anything.
2. If you can see text, include it. No exceptions.
3. Output ONLY the extracted text — no preamble, no commentary, no markdown formatting.`, pageIdx+1, len(images)),
			},
			{
				Type:     "image_url",
				ImageURL: &visionImageURL{URL: fmt.Sprintf("data:image/png;base64,%s", b64)},
			},
		}

		reqBody := visionRequest{
			Model:     model,
			MaxTokens: maxTokens,
			Messages: []visionMessage{
				{Role: "user", Content: content},
			},
		}

		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			return "", fmt.Errorf("marshal request page %d: %w", pageIdx+1, err)
		}

		url := fmt.Sprintf("%s/chat/completions", baseURL)
		req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
		if err != nil {
			return "", fmt.Errorf("create request page %d: %w", pageIdx+1, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

		client := &http.Client{Timeout: 180 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("vision API call page %d: %w", pageIdx+1, err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			return "", fmt.Errorf("vision API returned %d for page %d: %s", resp.StatusCode, pageIdx+1, string(body))
		}

		var vResp visionResponse
		if err := json.Unmarshal(body, &vResp); err != nil {
			return "", fmt.Errorf("parse response page %d: %w", pageIdx+1, err)
		}
		if vResp.Error != nil {
			return "", fmt.Errorf("API error page %d: %s", pageIdx+1, vResp.Error.Message)
		}
		if len(vResp.Choices) == 0 {
			return "", fmt.Errorf("no choices in response for page %d", pageIdx+1)
		}

		pageText := vResp.Choices[0].Message.Content
		log.Printf("vision extraction: page %d returned %d chars", pageIdx+1, len(pageText))

		if pageText != "" {
			if allText != "" {
				allText += "\n\n--- PAGE BREAK ---\n\n"
			}
			allText += pageText
		}
	}

	log.Printf("vision extraction: TOTAL %d chars across %d pages", len(allText), len(images))
	if len(allText) > 0 {
		preview := allText
		if len(preview) > 300 {
			preview = preview[:300]
		}
		log.Printf("vision extraction: preview: %s", preview)
	}

	return allText, nil
}

