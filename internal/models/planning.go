package models

import "time"

const PlanningDocumentSchemaV1 = "devcycle.planning-document/v1"

type PlanScope struct {
	Included []string `json:"included"`
	Excluded []string `json:"excluded"`
}

type PlanQuestion struct {
	Question         string `json:"question"`
	Blocking         bool   `json:"blocking"`
	SuggestedDefault string `json:"suggestedDefault,omitempty"`
}

type PlanCriterion struct {
	Description string `json:"description"`
	Rationale   string `json:"rationale"`
}

type PlanTestCase struct {
	Title     string   `json:"title"`
	Criterion string   `json:"criterion"`
	Kind      string   `json:"kind"`
	Setup     []string `json:"setup"`
	Steps     []string `json:"steps"`
	Expected  []string `json:"expected"`
}

type PlanTestStrategy struct {
	Summary      string   `json:"summary"`
	Environments []string `json:"environments"`
	Commands     []string `json:"commands"`
}

type PlanTask struct {
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	DependsOn            []string `json:"dependsOn"`
	Rationale            string   `json:"rationale"`
	Order                int      `json:"order"`
	SuggestedAdapter     string   `json:"suggestedAdapter,omitempty"`
	ExpectedDeliverables []string `json:"expectedDeliverables"`
}

type PlanRisk struct {
	Risk       string `json:"risk"`
	Severity   string `json:"severity"`
	Mitigation string `json:"mitigation"`
}

// PlanningDocument is the editable, provider-neutral delivery plan for one requirement.
// It is persisted independently from criteria/tasks so manual users and AI-assisted
// users share the same review and audit path.
type PlanningDocument struct {
	SchemaVersion    string           `json:"schemaVersion"`
	RequirementID    string           `json:"requirementId"`
	Understanding    string           `json:"understanding"`
	Scope            PlanScope        `json:"scope"`
	Assumptions      []string         `json:"assumptions"`
	OpenQuestions    []PlanQuestion   `json:"openQuestions"`
	Criteria         []PlanCriterion  `json:"criteria"`
	TestCases        []PlanTestCase   `json:"testCases"`
	TestStrategy     PlanTestStrategy `json:"testStrategy"`
	Tasks            []PlanTask       `json:"tasks"`
	Risks            []PlanRisk       `json:"risks"`
	RollbackConcerns []string         `json:"rollbackConcerns"`
	CandidateNotes   string           `json:"candidateNotes"`
	Source           string           `json:"source"`
	Provider         string           `json:"provider,omitempty"`
	Status           string           `json:"status"`
	Revision         int              `json:"revision"`
	CreatedAt        time.Time        `json:"createdAt,omitempty"`
	UpdatedAt        time.Time        `json:"updatedAt,omitempty"`
	AppliedAt        time.Time        `json:"appliedAt,omitempty"`
}
