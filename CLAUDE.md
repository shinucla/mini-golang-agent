# CLAUDE.md

## Version

The mga version is the constant `Current` in `internal/version/version.go`. The session list and `mga --version` show it.

Change the version in each commit that changes the behavior of mga. Use semantic versions:

- **Patch** (`v1.0.12` to `v1.0.13`): a bug fix, or a small change to the UI or to the text that mga shows.
- **Minor** (`v1.0.12` to `v1.1.0`): a new feature, command, flag, tool, or setting.
- **Major** (`v1.0.12` to `v2.0.0`): a change that breaks what users have now. Examples: a flag or command goes away, or mga cannot read old config or session files.

A commit that changes only the docs or the tests keeps the version.

Put the new version in the commit body as `Version: vX.Y.Z`. After the push to main, tag the commit with the version and push the tag: `git tag vX.Y.Z` and `git push origin vX.Y.Z`.
