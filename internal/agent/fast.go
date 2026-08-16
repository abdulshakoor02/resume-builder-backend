package agent

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	sdkmodel "github.com/pontus-devoteam/agent-sdk-go/pkg/model"
	"github.com/resume-builder/backend/internal/store"
)

var htmlDocRegex = regexp.MustCompile("(?is)<!DOCTYPE[^>]*>.*</html>")
var htmlTagRegex = regexp.MustCompile("(?is)<html.*</html>")

func extractHTML(s string) string {
	c := strings.TrimSpace(s)
	if strings.HasPrefix(c, "```") {
		if idx := strings.Index(c, "\n"); idx >= 0 {
			c = c[idx+1:]
		}
		if idx := strings.LastIndex(c, "```"); idx >= 0 {
			c = c[:idx]
		}
		c = strings.TrimSpace(c)
	}
	if m := htmlDocRegex.FindString(c); m != "" {
		return m
	}
	if m := htmlTagRegex.FindString(c); m != "" {
		return "<!DOCTYPE html>\n" + m
	}
	trimmed := strings.TrimSpace(c)
	if strings.Contains(strings.ToLower(trimmed), "<html") || strings.Contains(strings.ToLower(trimmed), "<!doctype") {
		return trimmed
	}
	if strings.Contains(trimmed, "<style") || strings.Contains(trimmed, "<div") {
		return trimmed
	}
	return ""
}

func (a *ResumeAgent) fastGenerate(ctx context.Context, systemPrompt, userInput string) (string, error) {
	if a.provider == nil {
		return "", fmt.Errorf("provider not configured")
	}
	modelName := a.provider.ModelName()
	if modelName == "" {
		modelName = "gpt-4o"
	}
	m, err := a.provider.GetProvider().GetModel(modelName)
	if err != nil {
		return "", fmt.Errorf("get model %s: %w", modelName, err)
	}
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	req := &sdkmodel.Request{
		SystemInstructions: systemPrompt,
		Input:              userInput,
		Settings:           &sdkmodel.Settings{},
	}
	start := time.Now()
	log.Printf("fastGenerate: calling model %s input_len=%d", modelName, len(userInput))
	resp, err := m.GetResponse(cctx, req)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("fastGenerate: error after %s: %v", elapsed, err)
		return "", err
	}
	log.Printf("fastGenerate: done in %s content_len=%d tool_calls=%d", elapsed, len(resp.Content), len(resp.ToolCalls))
	if len(resp.ToolCalls) > 0 {
		for _, tc := range resp.ToolCalls {
			if tc.Name == "generate_resume_html" {
				if html, ok := tc.Parameters["html"].(string); ok && len(html) > 500 {
					return html, nil
				}
			}
		}
	}
	content := resp.Content
	if html := extractHTML(content); html != "" {
		return html, nil
	}
	if len(content) > 1000 && strings.Contains(strings.ToLower(content), "<html") {
		return content, nil
	}
	if len(strings.TrimSpace(content)) > 500 {
		return content, nil
	}
	return "", fmt.Errorf("fastGenerate: no HTML found in response (len=%d)", len(content))
}

func (a *ResumeAgent) storeHTMLResult(userID, resumeID, html string) *AgentResult {
	revNum := 1
	key := fmt.Sprintf("html/%s/%s/v%d.html", userID, resumeID, revNum)
	store.PutHTML(key, []byte(html))
	store.PutHTML(resumeID, []byte(html))
	if a.ncStore != nil {
		go func() {
			if err := a.ncStore.UploadFile(key, []byte(html)); err != nil {
				log.Printf("fast store: nc upload failed %s: %v", key, err)
			}
		}()
	}
	return &AgentResult{
		HTMLPath:    key,
		ResumeData:  map[string]interface{}{"source": "fast_path", "html_size": len(html)},
		FinalOutput: html,
	}
}
