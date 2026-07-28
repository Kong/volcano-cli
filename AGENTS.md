# Agent instructions

## Documentation

Everything under `docs/` is **published** — CI syncs it to the public developer
docs site (`Kong/volcano-docs`). So `docs/` is for **end-user-facing docs only**:
the commands, flags, and behavior a Volcano user relies on.

**When you add or change user-facing behavior, update the matching doc under
`docs/` in the same PR.** Do **not** put internal or maintainer material in
`docs/` — dev-only env vars and overrides (e.g. pointing the CLI at a specific
API endpoint for local testing), design notes, or implementation detail. That
lives **next to the code it describes**: a code comment, or a notes/README file
in the relevant package — never under `docs/`, which is the sync target. When
unsure whether something is meant for users, keep it out of `docs/`.

Every doc must follow the Volcano docs format
(https://github.com/Kong/volcano-docs/blob/main/spec/markdown-format.md):

- YAML frontmatter with **`title`** and **`description`** (both required).
- **No H1 in the body** — the title comes from frontmatter; start at `##`.
- `kebab-case.md` filenames; `_index.md` (or `README.md`) for a section landing.
- Every fenced code block declares a language.

The shared lint (`.github/workflows/lint-docs.yml`) validates this on PRs that
touch `docs/`, and `internal/docs` parses the frontmatter for `volcano docs
search`, so malformed docs fail CI.
