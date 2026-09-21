package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/pontus-devoteam/agent-sdk-go/pkg/runner"
	"github.com/resume-builder/backend/internal/converter"
	"github.com/resume-builder/backend/internal/store"
	"github.com/resume-builder/backend/pkg/llm"
)

type ResumeAgent struct {
	runner   *runner.Runner
	provider *llm.ProviderFactory
	ncStore  *store.NextcloudStore
	anydoc   *converter.Client
}

func NewResumeAgent(cfg *llm.ProviderFactory, ncStore *store.NextcloudStore, anydoc *converter.Client) *ResumeAgent {
	r := runner.NewRunner()
	if cfg != nil {
		r.WithDefaultProvider(cfg.GetProvider())
	}
	return &ResumeAgent{
		runner:   r,
		provider: cfg,
		ncStore:  ncStore,
		anydoc:   anydoc,
	}
}

type AgentResult struct {
	HTMLPath    string                 `json:"html_path"`
	ResumeData  map[string]interface{} `json:"resume_data"`
	FinalOutput string                 `json:"final_output"`
}

func isFastPathEnabled() bool {
	v := os.Getenv("FAST_AGENT")
	if v == "0" || strings.EqualFold(v, "false") || strings.EqualFold(v, "off") {
		return false
	}
	return true
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...[truncated]..."
}

func countHistoryPrompts(history []map[string]string) int {
	n := 0
	for _, h := range history {
		if _, ok := h["prompt"]; ok {
			n++
		}
	}
	return n
}

func extractExistingHTML(history []map[string]string) string {
	for _, h := range history {
		if v, ok := h["html"]; ok && v != "" {
			return v
		}
	}
	return ""
}

// photoURLPattern matches the photo URL buildPhotoBlock hands the model
// (`<apiBase>/api/resumes/<24-hex id>/photo`), whatever apiBase this deployment
// uses.
var photoURLPattern = regexp.MustCompile(`https?://[^\s"'<>()]+/api/resumes/[0-9a-f]{24}/photo`)

// inlinePhotoReference replaces the photo URL inside generated HTML with the
// photo bytes as a data URI. The photo endpoint is owner-authenticated, and an
// <img> inside a saved or exported resume cannot carry a bearer token, so the
// document has to hold the image itself.
func inlinePhotoReference(html, resumeID, photoDataURI string) string {
	if html == "" || photoDataURI == "" {
		return html
	}
	if out := photoURLPattern.ReplaceAllString(html, photoDataURI); out != html {
		return out
	}
	// Model used a different form (relative path, HTML-escaped) — fall back to a
	// literal replace of the canonical URL for this resume.
	apiBase := os.Getenv("API_BASE_URL")
	if apiBase == "" {
		apiBase = "http://localhost:1100"
	}
	canonical := fmt.Sprintf("%s/api/resumes/%s/photo", strings.TrimRight(apiBase, "/"), resumeID)
	return strings.ReplaceAll(html, canonical, photoDataURI)
}

func buildPhotoBlock(resumeID, photoDataURI string) (string, string) {
	if photoDataURI == "" {
		return "", ""
	}
	log.Printf("agent: photo provided, data URI length=%d", len(photoDataURI))
	apiBase := os.Getenv("API_BASE_URL")
	if apiBase == "" {
		apiBase = "http://localhost:1100"
	}
	photoURL := fmt.Sprintf("%s/api/resumes/%s/photo", strings.TrimRight(apiBase, "/"), resumeID)
	block := fmt.Sprintf(
		"=== PROFILE PHOTO ===\nThe user provided a profile photo. Place it prominently in the HTML header area.\nUse an <img> tag with src=\"%s\" and alt=\"Profile Photo\".\nStyle it with: border-radius: 50%%; object-fit: cover; width: 110px; height: 110px;.\nThe photo must appear in the final HTML. Use the exact URL above.\n=== END PHOTO ===\n\n",
		photoURL,
	)
	return block, photoURL
}

func (a *ResumeAgent) GenerateResume(
	ctx context.Context,
	userID string,
	resumeID string,
	extractedText string,
	prompt string,
	conversationHistory []map[string]string,
	photoDataURI string,
) (*AgentResult, error) {
	if a.provider == nil {
		return nil, fmt.Errorf("LLM provider not configured - set LLM_API_KEY in your .env")
	}

	log.Printf("agent: building agent with prompt_len=%d extracted_text_len=%d history_count=%d",
		len(prompt), len(extractedText), len(conversationHistory))

	photoBlock, photoURL := buildPhotoBlock(resumeID, photoDataURI)

	// --- FAST PATH: single-turn direct LLM call (no tool loop) ---
	if isFastPathEnabled() {
		var fastInput string
		var useFast bool
		if len(conversationHistory) > 0 {
			// Refine: use existing HTML (truncated) + new prompt + photo block
			html := extractExistingHTML(conversationHistory)
			// If HTML is huge, truncate to keep input bounded
			htmlTrim := truncateStr(html, 25000)
			// Find structured context optionally for extra fidelity (truncated)
			var contextTrim string
			for _, h := range conversationHistory {
				if c, ok := h["context"]; ok && c != "" {
					contextTrim = truncateStr(c, 8000)
					break
				}
			}
			// Build lean refine input: one HTML + one new prompt + last 1 old prompt for tone
			var lastPrompt string
			if n := countHistoryPrompts(conversationHistory); n > 0 {
				for i := len(conversationHistory) - 1; i >= 0; i-- {
					if p, ok := conversationHistory[i]["prompt"]; ok && p != "" {
						lastPrompt = p
						break
					}
				}
				if lastPrompt != "" && lastPrompt != prompt {
					lastPrompt = "Last change: " + truncateStr(lastPrompt, 500) + "\n"
				} else {
					lastPrompt = ""
				}
			}
			fastInput = fmt.Sprintf("REFINE the resume below. Apply ONLY the requested change and return the FULL updated HTML document.\n\nUser request: %s\n\n%s=== EXISTING HTML (modify this, preserve everything else) ===\n%s\n=== END EXISTING HTML ===\n\n", prompt, lastPrompt, htmlTrim)
			if contextTrim != "" {
				fastInput += fmt.Sprintf("Existing structured data (reference): %s\n\n", contextTrim)
			}
			fastInput += "Return ONLY the complete updated HTML starting with <!DOCTYPE html>. Modify only what was requested. Preserve all other sections, dates, bullets, styling."
			if photoBlock != "" {
				fastInput = photoBlock + fastInput
			}
			useFast = html != "" && len(html) > 500
		} else if extractedText != "" && len(extractedText) > 50 {
			// Create from uploaded resume - single turn
			et := truncateStr(extractedText, 30000)
			fastInput = fmt.Sprintf("Create a beautiful HTML resume from the resume text below. Preserve EVERY detail.\n\nUser instructions: %s\n\n=== RESUME TEXT START ===\n%s\n=== RESUME TEXT END ===\n\n", prompt, et)
			// Do NOT append workflow tool steps - FastSystemPrompt tells model to output HTML directly
			if photoBlock != "" {
				fastInput = photoBlock + fastInput
			}
			if photoURL != "" {
				fastInput += fmt.Sprintf("\nProfile photo URL (use in header): %s\n", photoURL)
			}
			useFast = true
		} else if prompt != "" {
			fastInput = fmt.Sprintf("Create a beautiful HTML resume based on: %s\n", prompt)
			if photoBlock != "" {
				fastInput = photoBlock + fastInput
			}
			useFast = true
		}
		if useFast && fastInput != "" {
			log.Printf("agent: attempting fast path input_len=%d", len(fastInput))
			start := time.Now()
			html, err := a.fastGenerate(ctx, FastSystemPrompt, fastInput)
			fastElapsed := time.Since(start)
			if err == nil && len(html) > 1000 {
				log.Printf("agent: fast path succeeded in %s html_len=%d", fastElapsed, len(html))
				// Inline the photo so the stored document doesn't depend on an
				// authenticated URL (see inlinePhotoReference).
				if photoDataURI != "" {
					html = inlinePhotoReference(html, resumeID, photoDataURI)
				}
				// Determine revision-ish key: create = v1, refine = vN where N = prior count +1
				revHint := 1
				if len(conversationHistory) > 0 {
					revHint = countHistoryPrompts(conversationHistory) + 1
					if revHint < 2 {
						revHint = 2
					}
				}
				// Store with correct revision number
				key := fmt.Sprintf("html/%s/%s/v%d.html", userID, resumeID, revHint)
				store.PutHTML(key, []byte(html))
				store.PutHTML(resumeID, []byte(html))
				if a.ncStore != nil {
					go func(k string, data []byte) {
						if err := a.ncStore.UploadFile(k, data); err != nil {
							log.Printf("fast store: nc upload failed %s: %v", k, err)
						}
					}(key, []byte(html))
				}
				// Try to extract resume data from HTML for structured_data field (best-effort)
				var resumeData map[string]interface{}
				resumeData = map[string]interface{}{"source": "fast_path", "html_size": len(html), "elapsed_ms": fastElapsed.Milliseconds()}
				return &AgentResult{
					HTMLPath:    key,
					ResumeData:  resumeData,
					FinalOutput: html,
				}, nil
			}
			log.Printf("agent: fast path failed after %s: %v - falling back to agent loop", fastElapsed, err)
		}
	}

	// --- FALLBACK: Agentic loop with optimized prompts (fewer turns) ---
	agt := a.provider.CreateAgent("ResumeDesigner")
	agt.SetSystemInstructions(SystemPrompt)

	toolCtx := &ToolContext{
		NCStore:      a.ncStore,
		Anydoc:       a.anydoc,
		UserID:       userID,
		ResumeID:     resumeID,
		RevisionNum:  0,
		PhotoDataURI: photoDataURI,
	}

	tools := toolCtx.BuildTools()
	for _, t := range tools {
		agt.WithTools(t)
	}
	log.Printf("agent: %d tools registered", len(tools))

	var input string

	if len(conversationHistory) > 0 {
		// Optimized refine input: ONE html + ONE context + last 1 prompt (bounded)
		input = fmt.Sprintf("REFINE this resume based on: %s\n\n", prompt)
		html := extractExistingHTML(conversationHistory)
		if html != "" {
			html = truncateStr(html, 25000)
			input += fmt.Sprintf("=== EXISTING RESUME HTML (modify this, keep everything except the requested changes) ===\n%s\n=== END EXISTING HTML ===\n\n", html)
		}
		for _, rev := range conversationHistory {
			if c, ok := rev["context"]; ok && c != "" {
				input += fmt.Sprintf("=== EXISTING RESUME DATA (reference) ===\n%s\n=== END EXISTING DATA ===\n\n", truncateStr(c, 8000))
				break
			}
		}
		// Only last prompt, not all history
		var lastPrompt string
		for i := len(conversationHistory) - 1; i >= 0; i-- {
			if p, ok := conversationHistory[i]["prompt"]; ok && p != "" && p != prompt {
				lastPrompt = p
				break
			}
		}
		if lastPrompt != "" {
			input += fmt.Sprintf("Previous change: %s\n\n", truncateStr(lastPrompt, 500))
		}
		input += "Generate the complete updated HTML and call generate_resume_html(html=YOUR_HTML) with the full document. Preserve ALL existing content except the requested change."
	} else if extractedText != "" && len(extractedText) > 50 {
		et := truncateStr(extractedText, 30000)
		input = fmt.Sprintf("Create a beautiful HTML resume. Preserve EVERY detail from the resume text below.\n\nUser instructions: %s\n\n=== RESUME TEXT START ===\n%s\n=== RESUME TEXT END ===\n\nWrite the complete HTML and call generate_resume_html(html=YOUR_HTML). Do NOT fabricate. Preserve all sections, dates, bullets, skills.", prompt, et)
	} else {
		input = fmt.Sprintf("Create a beautiful HTML resume based on: %s\n\nWrite the complete HTML and call generate_resume_html().", prompt)
	}

	if photoBlock != "" {
		input = photoBlock + input
	}

	tStart := time.Now()
	result, err := a.runner.RunSync(agt, &runner.RunOptions{
		Input:    input,
		MaxTurns: 6,
	})
	log.Printf("agent: runner finished in %s", time.Since(tStart))

	if err != nil {
		log.Printf("agent: run failed: %v", err)
		return nil, fmt.Errorf("agent run: %w", err)
	}

	log.Printf("agent: finished, items=%d", len(result.NewItems))

	htmlPath := ""
	if toolCtx.RevisionNum > 0 {
		htmlPath = fmt.Sprintf("html/%s/%s/v%d.html", userID, resumeID, toolCtx.RevisionNum)
	} else {
		log.Printf("agent: WARNING - no tools called, agent returned text without generating HTML")
	}

	var finalOutput string
	if result.FinalOutput != nil {
		if s, ok := result.FinalOutput.(string); ok {
			finalOutput = s
		} else if b, err := json.Marshal(result.FinalOutput); err == nil {
			finalOutput = string(b)
		}
	}

	log.Printf("agent: LLM response (first 500 chars): %.500s", finalOutput)

	var resumeData map[string]interface{}
	if finalOutput != "" {
		if err := json.Unmarshal([]byte(finalOutput), &resumeData); err != nil {
			resumeData = map[string]interface{}{
				"raw_output": finalOutput,
			}
		}
	}

	return &AgentResult{
		HTMLPath:    htmlPath,
		ResumeData:  resumeData,
		FinalOutput: finalOutput,
	}, nil
}
