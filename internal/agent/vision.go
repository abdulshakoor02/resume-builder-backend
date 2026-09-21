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
		// FinishReason is "stop" for a complete reply and "length" when the
		// provider cut the model off mid-answer.
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// generationMaxTokens bounds the output of a multimodal generation.
//
// It must NOT inherit LLM_VISION_MAX_TOKENS: that variable sizes the scanned-PDF
// *extractor*, this deployment sets it to 100000, and an effectively unlimited
// budget on a generation is what slowed design-referenced builds down (the model
// padded documents to 18k+ characters where ~7k does). It must also stay
// comfortably above a full resume — a document that runs into the cap is cut off
// mid-markup and unusable. LLM_GEN_MAX_TOKENS overrides the default.
func generationMaxTokens() int {
	maxTokens := 12000
	if v := os.Getenv("LLM_GEN_MAX_TOKENS"); v != "" {
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
		MaxTokens: generationMaxTokens(),
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
	finish := parsed.Choices[0].FinishReason
	log.Printf("generateWithImage: model=%s max_tokens=%d image_chars=%d done in %s content_len=%d finish_reason=%q",
		model, generationMaxTokens(), len(imageDataURI), time.Since(start), len(content), finish)

	// A partial reply must never be stored: the user would get a resume that
	// breaks off in the middle of its own CSS (this happened in production). The
	// error sends the request down the text-only path, which returns a complete
	// document instead of half a designed one.
	if finish == "length" || (strings.Contains(strings.ToLower(content), "<html") && !looksComplete(extractHTML(content))) {
		return "", fmt.Errorf("multimodal reply was incomplete (finish_reason=%q, %d chars)", finish, len(content))
	}
	return content, nil
}

// looksComplete reports whether a reply holds a whole HTML document.
func looksComplete(html string) bool {
	return strings.Contains(strings.ToLower(html), "</html>")
}

// designSpecBlock formats the design read from a reference image.
func designSpecBlock(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}
	return "\n\n" + DesignSpecInstructions + "\n" + spec + "\n"
}

// generateDesignSpec reads a reference image once and writes its design down as
// text, so the document itself can be generated without the image attached.
func (a *ResumeAgent) generateDesignSpec(ctx context.Context, imageDataURI string) (string, error) {
	spec, err := a.generateWithImage(ctx, DesignSpecSystemPrompt, DesignSpecUserPrompt, imageDataURI)
	if err != nil {
		return "", err
	}
	spec = strings.TrimSpace(spec)
	// A runaway description would eat the document prompt it is inserted into.
	if len(spec) > maxDesignSpecChars {
		spec = spec[:maxDesignSpecChars]
	}
	return spec, nil
}

// maxDesignSpecChars caps the written-down design; it is asked for under 250
// words, so this only ever bites if the model ignores that.
const maxDesignSpecChars = 6000

// designRefPromptBlock formats the design-reference instructions for the user
// message. Empty when no reference image is attached, so the text-only path is
// byte-for-byte what it was before this feature.
func designRefPromptBlock(designRefDataURI string) string {
	if designRefDataURI == "" {
		return ""
	}
	return "\n\n" + DesignRefInstructions + "\n"
}
