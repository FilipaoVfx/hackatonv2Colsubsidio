package theme

import "github.com/charmbracelet/lipgloss"

// Source of truth: /root/guardian-ia-landing/src/index.css:44-70
const (
	Guardian     = lipgloss.Color("#ffe600")
	GuardianDeep = lipgloss.Color("#e6cf00")
	GuardianTint = lipgloss.Color("#2a2612")

	AI     = lipgloss.Color("#4f8bff")
	AIDeep = lipgloss.Color("#7fa9ff")
	AITint = lipgloss.Color("#16233d")

	Ink     = lipgloss.Color("#ffffff")
	Carbon  = lipgloss.Color("#121824")
	Cloud   = lipgloss.Color("#1a2233")
	Pizarra = lipgloss.Color("#a3aec4")
	Humo    = lipgloss.Color("#78859e")
	Line    = lipgloss.Color("#24304a")
	Live    = lipgloss.Color("#2bd576")
	Danger  = lipgloss.Color("#ff5c5c")
)

// EventColor maps a backend event Type to its semantic color. One vocabulary
// shared by Pipeline, Calls, and `secura tail`.
func EventColor(eventType string) lipgloss.Color {
	switch eventType {
	case "USER_SPOKE", "MESSAGE_RECEIVED", "TRANSCRIPT_UPDATED":
		return Ink
	case "LLM_REQUESTED", "LLM_RESPONSE", "KNOWLEDGE_RETRIEVED":
		return AI
	case "TOOL_CALLED", "TOOL_EXECUTED":
		return Guardian
	case "RECOMMENDATION_GENERATED", "LEAD_READY", "ENROLLMENT_CREATED", "QUOTE_CREATED":
		return Live
	case "ERROR_OCCURRED":
		return Danger
	default:
		return Pizarra
	}
}
