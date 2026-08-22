package models

import "time"

const (
	AIProviderOpenAICompatible = "openai_compatible"
	AIProviderAnthropic        = "anthropic"
	AIProviderGemini           = "gemini"
	AIProviderLocalOpenAI      = "local_openai"
	AIProviderCustomOpenAI     = "custom_openai"
)

// AIProviderConfig contains no credential value. SecretRef points to an OS
// credential-store entry or an environment variable and is never serialized to clients.
type AIProviderConfig struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Kind           string            `json:"kind"`
	BaseURL        string            `json:"baseUrl"`
	Model          string            `json:"model"`
	APIPath        string            `json:"apiPath,omitempty"`
	APIKeyHeader   string            `json:"apiKeyHeader,omitempty"`
	APIKeyPrefix   string            `json:"apiKeyPrefix,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Enabled        bool              `json:"enabled"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	SecretRef      string            `json:"-"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}
