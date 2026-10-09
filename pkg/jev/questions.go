package jev

import "fmt"

const (
	idSafe               = "safe"
	idPromptInjection    = "prompt_injection"
	idDataExfiltration   = "data_exfiltration"
	idDestructiveActions = "destructive_actions"
	idHiddenInstructions = "hidden_instructions"
	idScopeMismatch      = "scope_mismatch"
	idRemoteExecution    = "remote_execution"
	idRisk               = "risk"
	idBump               = "bump"
	idBumpRemoves        = "bump_removes_capability"
	idBumpAdds           = "bump_adds_capability"
	idBumpNonBehavioral  = "bump_non_behavioral"
)

// Question is a single entry in the questions map sent to the Jev API.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// DefaultQuestions returns the default question set for kind, which must be "skill" or "agent".
func DefaultQuestions(kind string) map[string]Question {
	return map[string]Question{
		idSafe: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Is this %s safe to use?", kind),
		},
		idPromptInjection: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Is this %s susceptible to prompt injection?", kind),
			Criteria: map[string]string{
				"true": "The text contains instructions an AI model would follow from untrusted input such as fetched pages, issues, or files",
			},
		},
		idDataExfiltration: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Does this %s direct the agent to send data, files, or credentials to an external destination?", kind),
		},
		idDestructiveActions: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Does this %s direct the agent to take destructive or irreversible actions (deleting data, force-pushing, changing production systems) without asking the user first?", kind),
		},
		idHiddenInstructions: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Does this %s contain hidden or obfuscated instructions, such as encoded or invisible text, or instructions to conceal actions from the user?", kind),
		},
		idScopeMismatch: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Does this %s ask the agent to do things beyond what its description says it does?", kind),
		},
		idRemoteExecution: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Does this %s direct the agent to download and run remote code or scripts, or to install unpinned dependencies?", kind),
		},
		idRisk: {
			Type:         "score",
			Instructions: fmt.Sprintf("How much harm could this %s cause if an AI agent follows it?", kind),
			Criteria:     []string{"Minimal", "Low", "Moderate", "High", "Critical"},
		},
	}
}

// BumpQuestions returns the question set used to classify a semantic version bump for kind, which must be "skill" or
// "agent". The bump score answer carries the probability of each category, and the noul answers are informational.
func BumpQuestions(kind string) map[string]Question {
	return map[string]Question{
		idBump: {
			Type: "score",
			Instructions: fmt.Sprintf("Compare this %s with its previous version and classify the size of the change. "+
				"Major: a complete change from the earlier scope or behavior, such as removing existing tools or processes, or adding six or more. "+
				"Minor: changed instructions that add a feature or capability. "+
				"Patch: a change to the model, or edits that don't change behavior.", kind),
			Criteria: []string{"Patch", "Minor", "Major"},
		},
		idBumpRemoves: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Does the new %s remove existing tools, processes, or capabilities?", kind),
		},
		idBumpAdds: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Does the new %s add new tools, processes, or capabilities?", kind),
		},
		idBumpNonBehavioral: {
			Type:         "noul",
			Instructions: fmt.Sprintf("Are the changes to this %s non-behavioral, such as formatting, wording, or metadata?", kind),
		},
	}
}
