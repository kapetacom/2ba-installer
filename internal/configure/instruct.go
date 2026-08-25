package configure

import (
	"path/filepath"
)

// InstructContinue prints manual steps for Continue (it has no machine-readable
// model block the installer owns).
func InstructContinue(e *Env) {
	if !dirExists(filepath.Join(home(), ".continue")) {
		return
	}
	e.warnf("Continue detected — add this model block to ~/.continue/config.json:")
	e.hintf("{\"title\": \"2ba.ai\", \"provider\": \"openai\", \"model\": \"%s\",", e.Model)
	e.hintf(" \"apiKey\": \"<your key from %s/api-keys>\", \"apiBase\": \"%s\"}", e.APIOrigin, e.APIBase)
}

// InstructCursor prints manual steps for Cursor (configured through the UI).
func InstructCursor(e *Env) {
	if !dirExists(filepath.Join(home(), ".cursor")) && !onPath("cursor") {
		return
	}
	e.warnf("Cursor detected — configure via UI: Settings → Models →")
	e.hintf("OpenAI API Key: <your key from %s/api-keys>", e.APIOrigin)
	e.hintf("Override base URL: %s   Model: %s", e.APIBase, e.Model)
}
