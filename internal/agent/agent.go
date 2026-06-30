package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/pontus-devoteam/agent-sdk-go/pkg/runner"
	"github.com/resume-builder/backend/internal/store"
	"github.com/resume-builder/backend/pkg/llm"
)

type ResumeAgent struct {
	runner   *runner.Runner
	provider *llm.ProviderFactory
	ncStore  *store.NextcloudStore
}

func NewResumeAgent(cfg *llm.ProviderFactory, ncStore *store.NextcloudStore) *ResumeAgent {
	r := runner.NewRunner()
	if cfg != nil {
		r.WithDefaultProvider(cfg.GetProvider())
	}
	return &ResumeAgent{
		runner:   r,
		provider: cfg,
		ncStore:  ncStore,
	}
}

type AgentResult struct {
	HTMLPath    string                 `json:"html_path"`
	ResumeData  map[string]interface{} `json:"resume_data"`
	FinalOutput string                 `json:"final_output"`
}

func (a *ResumeAgent) GenerateResume(
	ctx context.Context,
	userID string,
	resumeID string,
	extractedText string,
	prompt string,
	conversationHistory []map[string]string,
) (*AgentResult, error) {
	if a.provider == nil {
		return nil, fmt.Errorf("LLM provider not configured - set LLM_API_KEY in your .env")
	}

	log.Printf("agent: building agent with prompt_len=%d extracted_text_len=%d history_count=%d",
		len(prompt), len(extractedText), len(conversationHistory))

	agt := a.provider.CreateAgent("ResumeDesigner")
	agt.SetSystemInstructions(SystemPrompt)

	toolCtx := &ToolContext{
		NCStore:     a.ncStore,
		UserID:      userID,
		ResumeID:    resumeID,
		RevisionNum: 0,
	}

	tools := toolCtx.BuildTools()
	for _, t := range tools {
		agt.WithTools(t)
	}
	log.Printf("agent: %d tools registered", len(tools))

	var input string

	// Refinement flow: include existing structured data + previous prompts
	if len(conversationHistory) > 0 {
		input = fmt.Sprintf("REFINE this resume based on: %s\n\n", prompt)

		// Find and include the existing structured data
		for _, rev := range conversationHistory {
			if ctx, ok := rev["context"]; ok && ctx != "" {
				input += fmt.Sprintf("=== EXISTING RESUME DATA (MODIFY THIS, PRESERVE EVERY DETAIL) ===\n%s\n=== END EXISTING DATA ===\n\n", ctx)
				break
			}
		}

		// List previous refinement prompts
		input += "Previous refinements:"
		for i, rev := range conversationHistory {
			if p, ok := rev["prompt"]; ok && p != "" {
				input += fmt.Sprintf("\n%d: %s", i+1, p)
			}
		}

		input += "\n\nWORKFLOW:\nSTEP 1: Call get_resume_schema()\nSTEP 2: Call get_design_themes() and choose the best theme\nSTEP 3: Using the EXISTING resume data above, apply the refinement and construct the complete structured JSON. Do NOT omit any existing sections, dates, or bullet points — only modify what the user asked to change.\nSTEP 4: Write the complete HTML resume using the structured data and call generate_resume_html(html=YOUR_HTML)\n\nIMPORTANT: Keep ALL existing content from the resume data above. Only change what the user asked to refine."
	} else if extractedText != "" && len(extractedText) > 50 {
		input = fmt.Sprintf("=== RESUME DATA (PRESERVE EVERY DETAIL) ===\n\nThe following is the complete extracted text from the uploaded resume file. You MUST preserve every detail — every job, every date, every bullet point, every skill.\n\nUser instructions: %s\n\n=== EXTRACTED TEXT START ===\n%s\n=== EXTRACTED TEXT END ===\n\nWORKFLOW:\nSTEP 1: Call get_resume_schema()\nSTEP 2: Call get_design_themes() and choose the best theme\nSTEP 3: Call extract_resume_data(raw_text=EXTRACTED_TEXT_ABOVE)\nSTEP 4: Output the complete structured JSON with ALL sections and details. Do NOT skip any section, date, or bullet point.\nSTEP 5: Write the complete HTML resume using the structured data and call generate_resume_html(html=YOUR_HTML)\n\nThe HTML must render EVERY section from the source. Do NOT fabricate. Do NOT summarize.\nUse the 1M context window fully — include rich styling, detailed sections, and creative design elements.", prompt, extractedText)
	} else {
		input = fmt.Sprintf("Create a beautiful HTML resume based on: %s\n\nCALL get_resume_schema(), get_design_themes(), then write HTML and call generate_resume_html().", prompt)
	}

	result, err := a.runner.RunSync(agt, &runner.RunOptions{
		Input:    input,
		MaxTurns: 20,
	})

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
