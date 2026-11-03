package models

// ProviderBudget uses integer millionths of CNY. Unknown cost never authorizes a request.
type ProviderBudget struct {
	MetadataCalls          int    `json:"metadataCalls"`
	GenerationCalls        int    `json:"generationCalls"`
	Provider               string `json:"provider"`
	LimitMicros            int64  `json:"limitMicros"`
	InputMicrosPerMillion  int64  `json:"inputMicrosPerMillion"`
	OutputMicrosPerMillion int64  `json:"outputMicrosPerMillion"`
	MaxCalls               int    `json:"maxCalls"`
	MaxOutputTokens        int    `json:"maxOutputTokens"`
	Calls                  int    `json:"calls"`
	ChargedMicros          int64  `json:"chargedMicros"`
	Blocked                bool   `json:"blocked"`
}
type ProviderReservation struct {
	ID          string
	Provider    string
	Micros      int64
	InputBound  int
	OutputBound int
	InputRate   int64
	OutputRate  int64
}
