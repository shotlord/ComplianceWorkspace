# Compliance Workspace v1.0 — Agent Rules

## Purpose
Build a personal, local-first, offline, portable work/activity tracking web application for a Compliance Officer. Develop with fictional sample data only.

## Nonnegotiable technical constraints
- Backend: Go, standard-library net/http, database/sql, and pure-Go SQLite driver (`modernc.org/sqlite`).
- UI: embedded HTML/CSS/vanilla JavaScript through Go `embed`; no CDN, externally hosted fonts, tracking, or runtime API keys.
- Database: `data/compliance.db` resolved RELATIVE TO THE EXECUTABLE DIRECTORY (not working directory or user profile).
- Portable outputs: distinct native builds for Windows amd64/arm64, Linux amd64/arm64, and macOS amd64/arm64 as platform support permits. Do not claim a Windows EXE runs natively on other OSes.
- Bind HTTP ONLY to 127.0.0.1 on an OS-assigned free port. Validate Host/Origin; protect state-changing endpoints against CSRF. Never bind to 0.0.0.0.
- Store dates as YYYY-MM-DD; store event instants in UTC RFC3339, display in Asia/Jakarta by default with configurable timezone later.
- Use SQL parameter binding, input validation, HTML escaping, and secure defaults.
- Use numbered database migrations and transactions for each work-item change + history entry.
- Activity records are append-only in the app; edits must be corrections stored as NEW entries.
- No destructive delete of work items in v1; archive instead.
- No internet required at runtime; no auto updates; no cloud sync.
- Use SQLite rollback journal DELETE for portability and use SQLite-consistent backup (e.g., VACUUM INTO). Never copy a live SQLite DB naively.
- UI in Indonesian; accessible and responsive; light/dark mode optional.
- Fail with a clear actionable message if executable directory/data folder is not writable; NEVER silently move database location.
- On Windows, keep the app usable for a non-admin user (subject to organizational endpoint policies).
- No sensitive employer documents, secrets, or real personal data should be used during development.
- Provide unit/integration tests, build scripts, packaging, README, and verified acceptance test report.

## Domain model
Work item: title, description, category, requested_by, received_on, due_on, priority, status, completed_at, timestamps, archived_at, code (CMP-YYYY-NNNNNN; immutable; unique).
Activity log: linked work item, type, human-readable description, field name, old/new values, occurred_at and recorded_at.
Categories can start as free-form string values in v1.0.
Statuses: not_started, in_progress, waiting, completed, cancelled.
Priorities: low, normal, high, critical.
Use `schema.sql` as initial reference and apply it through migrations, not on every startup.

## Development workflow
First provide architecture, milestones, test plan, and a proposed file tree. Wait for explicit approval before performing file-system-changing actions. Work in small verified changes. Never claim tests pass unless executed. Ask before installing dependencies, running untrusted commands, deleting files, or accessing outside the project.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.
