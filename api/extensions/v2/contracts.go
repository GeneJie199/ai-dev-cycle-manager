// Package extensions defines public, optional extension boundaries.
// Community never imports an Enterprise implementation or contacts a license server.
package extensions

import "context"

const Version = "ai-devops.extensions/v2alpha1"

type Extension interface {
	ID() string
	Capabilities() []string
}
type Entitlements interface {
	Allowed(projectID, capability string) bool
}
type CommunityEntitlements struct{}

func (CommunityEntitlements) Allowed(string, string) bool { return true }

type DeploymentPlan struct {
	ProjectID      string `json:"projectId"`
	CandidateURI   string `json:"candidateUri"`
	EvidenceDigest string `json:"evidenceDigest"`
	Target         string `json:"target"`
	Environment    string `json:"environment"`
}

// DeploymentRuntime describes how to deploy. It cannot grant release approval.
type DeploymentRuntime interface {
	Extension
	Plan(context.Context, DeploymentPlan) ([]string, error)
}
type PolicyDecision struct {
	Allowed bool     `json:"allowed"`
	Reasons []string `json:"reasons"`
}
type PolicyExtension interface {
	Extension
	Evaluate(context.Context, string, string) (PolicyDecision, error)
}
