# Recommended tmux setup

Tmux is powerful out of the box, but a handful of default settings feel
broken to newcomers (tiny scrollback, no mouse, laggy escape key in
vim). This page is a curated set of recommendations — paste the blocks
you want into `~/.tmux.conf` and you'll have a setup that most tmux
users would consider the sensible baseline.

Everything here is optional. Bay itself does not require any of it; if
you've already run `bay setup`, bay's own keybindings live in a
separate block of your config and won't collide with anything below.

## A note on notation: "prefix + x"

Almost all tmux keybindings are two-step: you press a **prefix key**
first, release it, then press the command key. The default prefix is
`Ctrl+b`. So `prefix + d` means:

1. Hold `Ctrl` and press `b`
2. Release both
3. Press `d`

You can change the prefix to something more comfortable — see
[Choose your prefix key](#3-choose-your-prefix-key) below. Throughout
this page, `prefix + x` refers to whatever prefix you've configured
(default `Ctrl+b`).

## How to apply

1. Open (or create) `~/.tmux.conf` in your editor.
2. Paste the blocks below that you want.
3. Reload: inside tmux, press `prefix + :` and type
   `source-file ~/.tmux.conf`. Or, once you add the reload binding in
   the quality-of-life section, just `prefix + r`.

No running tmux yet? Start one with `tmux`. Detach with `prefix + d`
(the session keeps running in the background); reattach with
`tmux attach`.

## 1. Baseline — fix the defaults

These are the "tmux's defaults are wrong" settings. Near-universal
agreement; safe to paste without thinking.

```tmux
# Bigger scrollback buffer (default 2000 is tiny)
set -g history-limit 25000

# Click panes to focus, scroll with the wheel
set -g mouse on

# Windows and panes start at 1 (matches keyboard layout)
set -g base-index 1
setw -g pane-base-index 1

# When you close a window, renumber the rest so there are no gaps
set -g renumber-windows on

# Make <Esc> feel instant in vim/neovim
set -sg escape-time 10

# Let editors detect focus changes (for auto-reload on focus)
set-option -g focus-events on

# Copy to the system clipboard via OSC 52 (works over SSH)
set -s set-clipboard external

# True color support for modern terminal themes
set -g default-terminal "tmux-256color"
set-option -sa terminal-overrides ',xterm-256color:RGB'
```

## 2. Quality of life — strongly recommended

Opinionated but each one solves a real frustration.

```tmux
# Split with | and - (visually match what they do, unlike " and %)
unbind '"'
unbind %
bind | split-window -h
bind - split-window -v

# New windows open in the current directory, not $HOME
bind c new-window -c '#{pane_current_path}'

# Reload config with prefix + r
bind r {
  source-file ~/.tmux.conf
  display 'config reloaded'
}

# Window names auto-update to the current directory basename
set-option -g automatic-rename on
set-option -g automatic-rename-format '#{b:pane_current_path}'

# Clear the screen without losing history: prefix + s
# Clear screen AND scrollback history: prefix + S
bind s send-keys -R Enter
bind S {
  send-keys -R Enter
  clear-history
}
```

## 3. Choose your prefix key

Tmux's default prefix is `Ctrl+b`. Many users remap it — `Ctrl+b`
conflicts with vim's page-up, and it's an awkward two-finger stretch.
Common alternatives:

**Keep the default (`Ctrl+b`)** — fine if you don't use vim and don't
find it uncomfortable. No config needed.

**`Ctrl+a` (GNU screen style)** — most popular remap. Conflicts with
"start of line" in shells and emacs, so you'll need the `send-prefix`
line to pass it through when you actually want it.

```tmux
unbind C-b
set-option -g prefix C-a
bind-key C-a send-prefix
```

**`Ctrl+z`** — rarely-used shortcut for most devs (it suspends the
foreground process). Comfortable to reach. You lose `Ctrl+z` suspend
unless you bind `send-prefix`, which many people consider a feature.

```tmux
unbind C-b
set-option -g prefix C-z
bind-key C-z send-prefix
```

## 4. If you use bay

Run `bay setup` once. It adds a block of `Option`-prefixed keybindings
to `~/.tmux.conf` (bay picker, new bay, switch windows,
etc.) without touching anything above. No tmux prefix required for bay
keys — just `Option + <key>` directly.

See the [Keybindings](human-guide.md#keybindings) section of the human
guide for the full list.

## Going further

Official references:

- [tmux Getting Started guide](https://github.com/tmux/tmux/wiki/Getting-Started) — the official walkthrough
- [tmux man page](https://man.openbsd.org/tmux.1) — every option and command
- [tmux wiki](https://github.com/tmux/tmux/wiki) — FAQ, recipes, advanced topics

Community:

- [Awesome tmux](https://github.com/rothgar/awesome-tmux) — curated list of plugins and themes
- [tpm](https://github.com/tmux-plugins/tpm) — the standard plugin manager, if you want to add plugins later
