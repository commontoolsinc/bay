# Bay development

Bay is a Go CLI tool for managing concurrent workspaces with git worktrees and tmux.

## Installing during development

Bay manages its own workspaces, so changes to the CLI need to be installed to test interactively:

```
go install ./cmd/bay    # install your working copy
```

After merging, reinstall from main so you're not running stale code:

```
git checkout main && go install ./cmd/bay
```

## After implementation

After the initial implementation, review your changes: read the diff and check for code reuse (search for existing helpers before writing new ones), redundant state, copy-paste, and unnecessary work on hot paths. Fix any issues found.

## Before pushing

Run `scripts/presubmit.sh` before every `git push`. It mirrors CI: build, gofmt, vet, staticcheck, and tests with `-race`. Fix any failures before pushing.

## Testing

```
go test ./...           # run all tests
go test ./... -race     # with race detector (CI uses this)
go test ./internal/engine/ -run TestName  # single test
```

## Code style

- `gofmt` is enforced — CI rejects unformatted code
- `go vet` and `staticcheck` must pass
- No new lint suppressions without justification
