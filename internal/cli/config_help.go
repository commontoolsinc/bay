package cli

// configHelpText is the long-help body of the `bay config` parent
// command — describes the schema users hand-edit. Lives in its own
// file so the parent command code stays focused.
const configHelpText = `Bay Configuration — ~/.config/bay/config.toml

Bay's config file holds user preferences. Repos, docks, and workspaces
are tracked automatically in the manifest (~/.local/share/bay/manifest.json).
A config file is only needed for customization — bay works without one.

DEFAULTS

  default_agent = "claude"        # agent for 'bay agent' (probed on setup)
  default_editor = "cursor"       # editor for 'bay edit' (probed on setup)

  Set via CLI: 'bay config editor cursor' or 'bay setup'.

  Built-in agents: claude, codex, gemini. Built-in editors: cursor,
  code, zed, nvim, vim. These don't need config entries — bay knows
  their commands, resume args, and GUI detection.

CUSTOM AGENTS (optional)

  Override a built-in or add a new agent:

  [agents.my-agent]
  command = "my-agent-cli"
  resume_args = "--continue"      # added on recovery
  project_file = ".my-agent.md"   # checked by 'bay repo init'

CUSTOM EDITORS (optional)

  Set default_editor to any command on your PATH. Built-in editors
  (cursor, code, zed) are detected as GUI (fire-and-forget). All
  others are treated as terminal editors (tracked tmux pane).

  To mark a custom editor as GUI:

  [editors.sublime]
  gui = true

PER-DOCK OVERRIDES (optional)

  Override defaults for a specific dock:

  [docks.myproject]
  agent = "codex"                 # override default agent
  agent_args = ["--model", "o3"]  # override agent arguments
  terminal = "ghostty"            # host terminal app

MONITOR (optional)

  [monitor]
  interval_seconds = 5            # default: 3 seconds

EXAMPLE MINIMAL CONFIG

  An empty file works — bay auto-detects agents and editors.
  Run 'bay setup' to set preferences interactively.
`
