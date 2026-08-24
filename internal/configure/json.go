package configure

import (
	"bytes"
	"encoding/json"
	"os"
)

// writeIndentedJSON writes v as 2-space-indented JSON with a trailing newline
// and 0600 permissions, matching the python json.dump(indent=2) + write("\n")
// the historical install.sh used. HTML-significant characters are NOT escaped
// (SetEscapeHTML(false)) so keys such as api keys round-trip verbatim.
func writeIndentedJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil { // Encode appends the trailing newline
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
