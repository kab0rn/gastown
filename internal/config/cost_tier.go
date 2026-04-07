package config

import (
	"fmt"
	"os"
	"strings"
)

// CostTier represents a predefined cost optimization tier for model selection.
type CostTier string

const (
	// TierStandard uses opus for all roles (default, highest quality).
	TierStandard CostTier = "standard"
	// TierEconomy uses sonnet/haiku for patrol roles, keeps opus for workers.
	TierEconomy CostTier = "economy"
	// TierBudget uses haiku/sonnet for patrols, sonnet for workers.
	TierBudget CostTier = "budget"
	// TierCustomGroqOpus uses Opus for mayor/crew and Groq Compound for the rest.
	TierCustomGroqOpus CostTier = "custom-groq-opus"
)

// ValidCostTiers returns all valid tier names.
func ValidCostTiers() []string {
	return []string{
		string(TierStandard),
		string(TierEconomy),
		string(TierBudget),
		string(TierCustomGroqOpus),
	}
}

// IsValidTier checks if a string is a valid cost tier name.
func IsValidTier(tier string) bool {
	switch CostTier(tier) {
	case TierStandard, TierEconomy, TierBudget, TierCustomGroqOpus:
		return true
	default:
		return false
	}
}

// TierManagedRoles is the set of roles whose model selection is managed by cost tiers.
// These are the only roles that ApplyCostTier modifies — any other custom RoleAgents
// entries (e.g., user-defined roles or non-Claude agents for non-tier roles) are preserved.
//
// "boot" and "dog" are utility roles that should always use the cheapest model.
var TierManagedRoles = []string{"mayor", "deacon", "witness", "refinery", "polecat", "crew", "boot", "dog"}

// CostTierRoleAgents returns the role_agents mapping for a given tier.
// All tiers explicitly map every tier-managed role. Standard tier maps roles
// to empty string when they should use the default/opus model.
func CostTierRoleAgents(tier CostTier) map[string]string {
	switch tier {
	case TierStandard:
		return map[string]string{
			"mayor":    "",
			"deacon":   "",
			"witness":  "",
			"refinery": "",
			"polecat":  "",
			"crew":     "",
			"boot":     "claude-haiku",
			"dog":      "claude-haiku",
		}

	case TierEconomy:
		return map[string]string{
			"mayor":    "claude-sonnet",
			"deacon":   "claude-haiku",
			"witness":  "claude-sonnet",
			"refinery": "claude-sonnet",
			"polecat":  "",
			"crew":     "",
			"boot":     "claude-haiku",
			"dog":      "claude-haiku",
		}

	case TierBudget:
		return map[string]string{
			"mayor":    "claude-sonnet",
			"deacon":   "claude-haiku",
			"witness":  "claude-haiku",
			"refinery": "claude-haiku",
			"polecat":  "claude-sonnet",
			"crew":     "claude-sonnet",
			"boot":     "claude-haiku",
			"dog":      "claude-haiku",
		}

	case TierCustomGroqOpus:
		return map[string]string{
			"mayor":    "",
			"deacon":   "groq-compound",
			"witness":  "groq-compound",
			"refinery": "groq-compound",
			"polecat":  "groq-compound",
			"crew":     "",
			"boot":     "groq-compound",
			"dog":      "groq-compound",
		}

	default:
		return nil
	}
}

// CostTierAgents returns the custom agent definitions needed for a given tier.
func CostTierAgents(tier CostTier) map[string]*RuntimeConfig {
	switch tier {
	case TierStandard:
		return map[string]*RuntimeConfig{}
	case TierEconomy, TierBudget:
		return map[string]*RuntimeConfig{
			"claude-sonnet": claudeSonnetPreset(),
			"claude-haiku":  claudeHaikuPreset(),
		}
	case TierCustomGroqOpus:
		return map[string]*RuntimeConfig{
			"groq-compound": groqCompoundPreset(),
		}
	default:
		return nil
	}
}

// claudeSonnetPreset returns a RuntimeConfig for Claude Sonnet.
func claudeSonnetPreset() *RuntimeConfig {
	return &RuntimeConfig{
		Provider: string(AgentClaude),
		Command:  "claude",
		Args:     []string{"--dangerously-skip-permissions", "--model", "sonnet"},
	}
}

// claudeHaikuPreset returns a RuntimeConfig for Claude Haiku.
func claudeHaikuPreset() *RuntimeConfig {
	return &RuntimeConfig{
		Provider: string(AgentClaude),
		Command:  "claude",
		Args:     []string{"--dangerously-skip-permissions", "--model", "haiku"},
	}
}

// groqCompoundPreset returns a RuntimeConfig for the Groq Compound model.
// Routes through Groq's OpenAI-compatible API using the Claude CLI with env var overrides.
func groqCompoundPreset() *RuntimeConfig {
	return &RuntimeConfig{
		Command: "claude",
		Args:    []string{"--dangerously-skip-permissions"},
		Env: map[string]string{
			"ANTHROPIC_BASE_URL": "https://api.groq.com/openai/v1",
			"ANTHROPIC_API_KEY":  os.Getenv("GROQ_API_KEY"),
			"ANTHROPIC_MODEL":    "compound-beta",
		},
	}
}

// ApplyCostTier writes the tier's agent and role_agents configuration to town settings.
// Only tier-managed roles are modified — custom RoleAgents entries for non-tier roles
// (or intentional non-Claude overrides) are preserved.
func ApplyCostTier(settings *TownSettings, tier CostTier) error {
	roleAgents := CostTierRoleAgents(tier)
	if roleAgents == nil {
		return fmt.Errorf("invalid cost tier: %q (valid: %s)", tier, strings.Join(ValidCostTiers(), ", "))
	}

	agents := CostTierAgents(tier)

	if settings.RoleAgents == nil {
		settings.RoleAgents = make(map[string]string)
	}

	for _, role := range TierManagedRoles {
		agentName := roleAgents[role]
		if agentName == "" {
			delete(settings.RoleAgents, role)
		} else {
			settings.RoleAgents[role] = agentName
		}
	}

	if settings.Agents == nil {
		settings.Agents = make(map[string]*RuntimeConfig)
	}

	if tier == TierStandard {
		delete(settings.Agents, "claude-sonnet")
		delete(settings.Agents, "claude-haiku")
		delete(settings.Agents, "groq-compound")
	} else {
		for name, rc := range agents {
			settings.Agents[name] = rc
		}
	}

	settings.CostTier = string(tier)
	return nil
}

// GetCurrentTier infers the current cost tier from the settings' RoleAgents.
// Returns the tier name if it matches a known tier exactly, or empty string for custom configs.
func GetCurrentTier(settings *TownSettings) string {
	if settings.CostTier != "" && IsValidTier(settings.CostTier) {
		expected := CostTierRoleAgents(CostTier(settings.CostTier))
		if tierRolesMatch(settings.RoleAgents, expected) {
			return settings.CostTier
		}
	}

	for _, tierName := range ValidCostTiers() {
		tier := CostTier(tierName)
		expected := CostTierRoleAgents(tier)
		if tierRolesMatch(settings.RoleAgents, expected) {
			return tierName
		}
	}

	return ""
}

// tierRolesMatch checks if the actual RoleAgents map matches a tier's expected
// assignments for tier-managed roles only.
func tierRolesMatch(actual, expected map[string]string) bool {
	for _, role := range TierManagedRoles {
		actualVal := actual[role]
		expectedVal := expected[role]
		if actualVal != expectedVal {
			return false
		}
	}
	return true
}

// TierDescription returns a human-readable description of the tier's model assignments.
func TierDescription(tier CostTier) string {
	switch tier {
	case TierStandard:
		return "All roles use Opus (highest quality)"
	case TierEconomy:
		return "Patrol roles use Sonnet/Haiku, workers use Opus"
	case TierBudget:
		return "Patrol roles use Haiku, workers use Sonnet"
	case TierCustomGroqOpus:
		return "Mayor and Crew use Opus; the rest use Groq Compound"
	default:
		return "Unknown tier"
	}
}

// FormatTierRoleTable returns a formatted string showing role→model assignments for a tier.
func FormatTierRoleTable(tier CostTier) string {
	roleAgents := CostTierRoleAgents(tier)
	if roleAgents == nil {
		return ""
	}

	roles := []string{"mayor", "deacon", "witness", "refinery", "polecat", "crew", "boot", "dog"}
	var lines []string
	for _, role := range roles {
		agent := roleAgents[role]
		if agent == "" {
			agent = "(default/opus)"
		}
		lines = append(lines, fmt.Sprintf(" %-10s %s", role+":", agent))
	}

	return strings.Join(lines, "\n")
}
