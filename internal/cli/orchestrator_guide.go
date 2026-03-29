package cli

// orchestratorGuide is installed to ~/.config/bay/orchestrator-guide.md
// by bay setup. Orchestrator agents include this via --add-dir ~/.config/bay.
const orchestratorGuide = `# Bay — Orchestrator Guide

Bay manages concurrent workspaces across tmux windows and git worktrees.
Use these commands to create, monitor, and manage agent workspaces.

## Quick reference

### Setup

    bay repo add <name> <path>                     # register a local repo
    bay repo add <name> <path> --url <git-url>     # clone and register
    bay dock new <name> --repo <r> --agent <a>     # create a dock (tmux session)

### Create workspaces

    bay ws new [dock]                              # worktree + agent
    bay ws new [dock] --shell                      # worktree + shell
    bay ws new [dock] --name <n>                   # with display name
    bay ws new [dock] --repo <r>                   # override dock repo
    bay ws new [dock] --agent <a>                  # override dock agent

### Monitor

    bay ls                                         # all repos/docks/workspaces
    bay ws show <name>                             # workspace details

### Navigate

    bay go                                         # fzf picker
    bay go <query>                                 # jump by name/branch/PR
    bay go --next-waiting                          # cycle through waiting agents

### Close and clean up

    bay ws close <name>                            # safety checks first
    bay ws close <name> --force                    # skip checks
    bay dock close <name>                          # close all in dock
    bay repo remove <name>                         # remove repo config
    bay repo remove <name> --force                 # also remove docks

### Recovery

    bay recover                                    # reconstruct after reboot

## Concepts

- **Repo**: a local git checkout bay creates worktrees from.
- **Dock**: a tmux session grouping workspaces, with default repo and agent.
- **Workspace**: an isolated worktree + tmux window(s). Has an ID (w1, w2)
  and a display name (auto-abbreviated from branch).
- **Status**: idle (fresh), active (branch set), done (ready to close).

## Typical orchestrator workflow

    # Create workspaces for tasks
    bay ws new dev --name auth-fix
    bay ws new dev --name update-deps

    # Check progress
    bay ls

    # Close completed work
    bay ws close auth-fix

    # After reboot
    bay recover

## Workspace agents

Agents inside workspaces automatically receive bay instructions in their
config file. They can update their own metadata:

    !bay ws update self --branch <branch> --pr <number>
    !bay ws update self --status done

You do not need to teach workspace agents about bay — it happens
automatically when the workspace is created.
`
