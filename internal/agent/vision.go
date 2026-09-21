package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Multimodal generation for design references.
//
// The agent SDK it is built on (v0.0.9) cannot attach images: Request.Input is a
// plain string and its OpenAI provider has no image support, so - exactly as
// internal/parser does for scanned PDFs - a reference-image generation is posted
// straight to the chat-completions API with the image as an image_url content
// part. Callers must treat a failure here as recoverable and fall back to the
// text-only path, so a bad or unsupported image can never break generation.

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role string `json:"role"`
	// Either a plain string or []chatContentPart (multimodal).
	Content interface{} `json:"content"`
}

type chatContentPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *chatContentImage `json:"image_url,omitempty"`
}

type chatContentImage struct {
	URL string `json:"url"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// visionMaxTokens mirrors the scanned-PDF extractor: resume-shaped HTML is large,
// so the default output window has to be generous or the document gets truncated.
func visionMaxTokens() int {
	maxTokens := 16000
	if v := os.Getenv("LLM_VISION_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTokens = n
		}
	}
	return maxTokens
}

// generateWithImage posts the system prompt, the user prompt and one reference
// image to the configured model and returns the model's raw reply.
func (a *ResumeAgent) generateWithImage(ctx context.Context, systemPrompt, userInput, imageDataURI string) (string, error) {
	if imageDataURI == "" {
		return "", fmt.Errorf("no reference image provided")
	}
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("LLM_API_KEY not set")
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gpt-4o"
	}
	baseURL := strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	body := chatRequest{
		Model:     model,
		MaxTokens: visionMaxTokens(),
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: []chatContentPart{
				{Type: "text", Text: userInput},
				{Type: "image_url", ImageURL: &chatContentImage{URL: imageDataURI}},
			}},
		},
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal multimodal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create multimodal request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	start := time.Now()
	resp, err := (&http.Client{Timeout: 300 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("multimodal request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read multimodal response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(raw)
		if len(msg) > 400 {
			msg = msg[:400]
		}
		return "", fmt.Errorf("multimodal request returned HTTP %d: %s", resp.StatusCode, msg)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode multimodal response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("model error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("multimodal response had no choices")
	}

	content := parsed.Choices[0].Message.Content
	log.Printf("generateWithImage: model=%s image_chars=%d done in %s content_len=%d",
		model, len(imageDataURI), time.Since(start), len(content))
	return content, nil
}

// designRefPromptBlock formats the design-reference instructions for the user
// message. Empty when no reference image is attached, so the text-only path is
// byte-for-byte what it was before this feature.
func designRefPromptBlock(designRefDataURI string) string {
	if designRefDataURI == "" {
		return ""
	}
	return "\n\n" + DesignRefInstructions + "\n"
}
