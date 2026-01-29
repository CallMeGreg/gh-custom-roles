# Copilot Custom Instructions: GitHub CLI (gh) Extensions in Go

You are helping build GitHub CLI (gh) extensions in Go. Follow these practices:

## Go project structure (simple and idiomatic)
- Keep the folder structure minimal and Go-idiomatic.
- Prefer:
  - `cmd/gh-<ext>/main.go` for the entrypoint
  - `internal/...` for non-exported packages
  - `pkg/...` only if you need exported, reusable packages
- Avoid unnecessary layers, frameworks, or abstractions.

## CLI UX and interaction (pterm only)
- Use `pterm` for **all** CLI visuals and user interaction.
- Use `pterm` for:
  - Prompts and interactive input
  - Progress indicators and spinners
  - Tables, sections, and formatted output
  - Logging and status messages

## Flags first, interactive fallback
- Always accept values via flags.
- If a value is missing, prompt for it with an interactive `pterm` experience.
- Do not mix non-`pterm` input/output methods (no `fmt.Printf`, no `bufio` prompts).

## Progress and pagination
- When processing paginated requests, show progress with `pterm` (e.g., spinner or progress bar).
- When processing loops of requests, show progress with `pterm`.

## GitHub Enterprise support scope
- The extension must support both Enterprise Cloud and Enterprise Server.
- The primary target is Enterprise Server 3.15.
- If a feature requires a higher Enterprise Server version or Enterprise Cloud only, call it out explicitly.

## API documentation verification (required)
- ALWAYS fetch GitHub docs when working with APIs to validate paths, inputs, and response details.
- Example references:
  - Create a custom role: https://docs.github.com/en/enterprise-server@3.15/rest/orgs/custom-roles?apiVersion=2022-11-28
  - List repository fine-grained permissions for an organization: https://docs.github.com/en/enterprise-server@3.15/rest/orgs/custom-roles?apiVersion=2022-11-28#list-repository-fine-grained-permissions-for-an-organization

## Logging
- Use `pterm` for all logging (info, warnings, errors, success).
- Keep logs concise and user-friendly.
