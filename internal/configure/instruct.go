package configure

import (
	"fmt"
	"path/filepath"
)

// InstructContinue prints manual steps for Continue (it has no machine-readable
// model block the installer owns).
func InstructContinue(e *Env) {
	if !dirExists(filepath.Join(home(), ".continue")) {
		return
	}
	e.warnf("Continue detected — add this model block to ~/.continue/config.json:")
	fmt.Fprintf(e.Out, "    {\"title\": \"2ba.ai\", \"provider\": \"openai\", \"model\": \"%s\",\n", e.Model)
	fmt.Fprintf(e.Out, "     \"apiKey\": \"<your key from %s/api-keys>\", \"apiBase\": \"%s\"}\n", e.APIOrigin, e.APIBase)
}

// InstructCursor prints manual steps for Cursor (configured through the UI).
func InstructCursor(e *Env) {
	if !dirExists(filepath.Join(home(), ".cursor")) && !onPath("cursor") {
		return
	}
	e.warnf("Cursor detected — configure via UI: Settings → Models →")
	e.warnf("  OpenAI API Key: <your key from %s/api-keys>", e.APIOrigin)
	e.warnf("  Override base URL: %s   Model: %s", e.APIBase, e.Model)
}
