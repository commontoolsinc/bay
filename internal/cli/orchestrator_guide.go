package cli

import _ "embed"

// orchestratorGuide is installed to ~/.config/bay/orchestrator-guide.md
// by bay setup. Orchestrator agents include this via --add-dir ~/.config/bay.
//
//go:embed orchestrator-guide.md
var orchestratorGuide string
