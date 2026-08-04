package converter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/resume-builder/backend/internal/parser"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *Client) Convert(ctx context.Context, data []byte, filename string) (string, error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("anydoc URL is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/convert", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create anydoc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Filename", filename)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call anydoc: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024+1))
	if err != nil {
		return "", fmt.Errorf("read anydoc response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anydoc returned %s: %s", resp.Status, responseMessage(body))
	}
	if len(body) == 0 {
		return "", fmt.Errorf("anydoc returned empty markdown")
	}
	return string(body), nil
}

func (c *Client) ConvertWithFallback(ctx context.Context, data []byte, filename string) (string, error) {
	markdown, err := c.Convert(ctx, data, filename)
	if err == nil {
		return markdown, nil
	}
	if strings.EqualFold(fileExtension(filename), ".pdf") {
		ocrText, ocrErr := parser.ExtractPDFTextWithVision(data)
		if ocrErr != nil {
			return "", fmt.Errorf("anydoc conversion failed: %v; OCR fallback failed: %w", err, ocrErr)
		}
		if ocrText != "" {
			return ocrText, nil
		}
	}
	return "", err
}

func responseMessage(body []byte) string {
	if len(body) > 1000 {
		return string(body[:1000])
	}
	return string(body)
}

func fileExtension(filename string) string {
	idx := strings.LastIndexByte(filename, '.')
	if idx < 0 {
		return ""
	}
	return filename[idx:]
}
