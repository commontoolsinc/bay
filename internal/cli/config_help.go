package cli

// configHelpText is the long-help body of the `bay config` parent
// command — describes the schema users hand-edit. Lives in its own
// file so the parent command code stays focused.
const configHelpText = `Bay Configuration — ~/.config/bay/config.toml

Bay's config file holds user preferences and optional overrides.
Repos, docks, and workspaces are tracked automatically in the manifest
(~/.local/share/bay/manifest.json). A config file is only needed for
customization — bay works without one.

AGENTS

  Define AI agents bay can launch in workspaces.

  [agents.claude]
  command = "claude"
  resume_args = "--continue"      # added on restart/recovery
  project_file = "CLAUDE.md"      # checked by 'bay repo init'

  [agents.codex]
  command = "codex"

  Fields:
    command       what bay runs in the terminal
    resume_args   flags added when restarting (not on first launch)
    project_file  file bay checks for bay awareness (bay repo init)

EDITOR

  Bay auto-detects your editor (cursor, code, zed, nvim, vim).
  Set a preference to skip detection:

  [editor]
  command = "cursor"
  gui = true                      # optional; auto-detected from command

  Or via the CLI: 'bay config editor cursor'.

PER-DOCK OVERRIDES

  Override defaults for a specific dock. Only set fields you want
  to change — omitted fields use the dock's creation-time defaults.

  [docks.myproject]
  agent = "codex"                 # override default agent
  agent_args = ["--model", "o3"]  # override agent arguments
  terminal = "ghostty"            # host terminal app

MONITOR

  [monitor]
  interval_seconds = 3            # how often to check for waiting agents

EXAMPLE MINIMAL CONFIG

  [agents.claude]
  command = "claude"
  project_file = "CLAUDE.md"

That's enough. Everything else is optional or auto-detected.
`
