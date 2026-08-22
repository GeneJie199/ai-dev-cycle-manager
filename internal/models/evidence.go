package models

const (
	EvidenceKindCode              = "code"
	EvidenceKindCommit            = "commit"
	EvidenceKindTest              = "test"
	EvidenceKindScreenshot        = "screenshot"
	EvidenceKindCommand           = "command"
	EvidenceKindHTTPResponse      = "http_response"
	EvidenceKindArtifact          = "artifact"
	EvidenceKindHumanConfirmation = "human_confirmation"
)

// NormalizeEvidenceKind accepts v0.x names while storing the v1 canonical kind.
func NormalizeEvidenceKind(kind string) (string, bool) {
	switch kind {
	case EvidenceKindCode, EvidenceKindCommit, EvidenceKindTest, EvidenceKindScreenshot, EvidenceKindCommand, EvidenceKindHTTPResponse, EvidenceKindArtifact, EvidenceKindHumanConfirmation:
		return kind, true
	case "build", "lint", "log":
		return EvidenceKindCommand, true
	case "report":
		return EvidenceKindArtifact, true
	case "manual":
		return EvidenceKindHumanConfirmation, true
	default:
		return "", false
	}
}
