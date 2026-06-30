package llm

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	agent "github.com/pontus-devoteam/agent-sdk-go/pkg/agent"
	"github.com/pontus-devoteam/agent-sdk-go/pkg/model"
	"github.com/pontus-devoteam/agent-sdk-go/pkg/model/providers/openai"
)

type ProviderFactory struct {
	apiKey   string
	model    string
	baseURL  string
	provider *openai.Provider
}

func NewProviderFactory(apiKey, modelName, baseURL string) (*ProviderFactory, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("LLM_API_KEY is required")
	}

	p := openai.NewProvider(apiKey)
	p.SetDefaultModel(modelName)
	if baseURL != "" {
		p.SetBaseURL(baseURL)
	}

	// Override default 120s timeout with a configurable value.
	// Refinement calls with large structured data can take longer.
	timeout := 300 * time.Second
	if v := os.Getenv("LLM_HTTP_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	p.WithHTTPClient(&http.Client{Timeout: timeout})

	return &ProviderFactory{
		apiKey:   apiKey,
		model:    modelName,
		baseURL:  baseURL,
		provider: p,
	}, nil
}

func (f *ProviderFactory) CreateAgent(name string) *agent.Agent {
	a := agent.NewAgent(name)
	a.SetModelProvider(f.provider)
	a.WithModel(f.model)
	return a
}

func (f *ProviderFactory) GetProvider() model.Provider {
	return f.provider
}
