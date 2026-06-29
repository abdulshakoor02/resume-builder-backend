package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/pontus-devoteam/agent-sdk-go/pkg/runner"
	"github.com/resume-builder/backend/internal/generator"
	"github.com/resume-builder/backend/internal/store"
	"github.com/resume-builder/backend/pkg/llm"
)

type ResumeAgent struct {
	runner   *runner.Runner
	provider *llm.ProviderFactory
	gen      *generator.Generator
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
		gen:      generator.NewGenerator(),
		ncStore:  ncStore,
	}
}

type AgentResult struct {
	PDFPath     string                 `json:"pdf_path"`
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
		Generator:   a.gen,
		UserID:      userID,
		ResumeID:    resumeID,
		RevisionNum: 0,
	}

	if len(conversationHistory) > 0 {
		for range conversationHistory {
			toolCtx.RevisionNum++
		}
	}

	tools := toolCtx.BuildTools()
	for _, t := range tools {
		agt.WithTools(t)
	}
	log.Printf("agent: %d tools registered", len(tools))

	var input string
	if extractedText != "" && len(extractedText) > 50 {
		input = fmt.Sprintf("The user uploaded a resume file. Below is the extracted text. Parse it, structure it, and generate a professional PDF resume.\n\nUser instructions: %s\n\n=== EXTRACTED RESUME TEXT ===\n%s\n=== END EXTRACTED TEXT ===\n\nCALL get_resume_schema() and get_layout_templates() first, then structure this data and call generate_resume_pdf(). DO NOT ask questions. DO NOT reply with text. CALL THE TOOLS.", prompt, extractedText)
	} else {
		input = fmt.Sprintf("Generate a professional resume based on this description. Do NOT ask questions or chat. Just call the tools and generate the PDF.\n\nUser description: %s\n\nCALL get_resume_schema() and get_layout_templates() first, then create resume data from the description and call generate_resume_pdf(). DO NOT reply with text. CALL THE TOOLS.", prompt)
	}

	if len(conversationHistory) > 0 {
		input += "\n\nPrevious refinements:"
		for i, rev := range conversationHistory {
			input += fmt.Sprintf("\n%d: %s", i+1, rev["prompt"])
		}
		input += "\n\nApply this refinement and regenerate. CALL generate_resume_pdf()."
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

	pdfPath := ""
	if toolCtx.RevisionNum > 0 {
		pdfPath = fmt.Sprintf("outputs/%s/%s/v%d.pdf", userID, resumeID, toolCtx.RevisionNum)
	} else {
		log.Printf("agent: WARNING - no tools called, agent returned text without generating PDF")
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
		PDFPath:     pdfPath,
		ResumeData:  resumeData,
		FinalOutput: finalOutput,
	}, nil
}
