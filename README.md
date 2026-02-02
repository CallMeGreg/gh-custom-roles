# gh-custom-roles

GitHub CLI extension to create custom repository roles in one or more organizations.

## Features

- Create custom repository roles across single or multiple organizations
- Support for GitHub Enterprise Server and Enterprise Cloud
- Interactive prompts when inputs are not provided via flags
- Batch creation with progress tracking
- Confirmation step with summary and replication command
- CSV file support for targeting multiple organizations
- Skips missing orgs and existing roles with warnings

## Prerequisites

1. [GitHub CLI](https://github.com/cli/cli#installation)
2. Confirm that you are authenticated with an account that has access to the enterprise and organizations you would like to interact with. You can check your authentication status by running:

```
gh auth status
```

Ensure that you have the necessary scopes (`read:enterprise` and `admin:org`). You can add scopes by running:

```
gh auth login -s "read:enterprise,admin:org"
```

> [!IMPORTANT]
> Enterprise admins do not inherently have access to all of the organizations in the enterprise. You must ensure that your account has the necessary permissions to access the organizations you want to modify. To elevate your permissions for an organization, refer to these [GitHub docs](https://docs.github.com/en/enterprise-server@3.15/admin/managing-accounts-and-repositories/managing-organizations-in-your-enterprise/managing-your-role-in-an-organization-owned-by-your-enterprise).


## Installation

Install the extension via GitHub CLI:
```bash
gh extension install callmegreg/gh-custom-roles
```

## Usage

### Interactive mode (recommended)

Run the extension and follow the prompts:

```bash
gh custom-roles create
```

The extension will prompt you for:
1. GitHub hostname (defaults to `github.com`)
2. Target selection: single organization, all organizations in enterprise, or CSV file
3. Enterprise slug (only if targeting all organizations, defaults to `github`)
4. Custom role name and optional description
5. Base role (read, triage, write, maintain)
6. Fine-grained permissions (with descriptions shown)
7. Confirmation before creation

It will then display a summary and a ready-to-run replication command.

### Non-interactive mode with flags

For automation, provide all values via flags:

```bash
gh custom-roles create \
  --hostname github.com \
  --enterprise my-enterprise \
  --org myorg \
  --role-name "Secret Scanning Resolver" \
  --role-description "Developers who can view and resolve secret scanning alerts" \
  --base-role write \
  --permissions "view_secret_scanning_alerts,resolve_secret_scanning_alerts"
```

### Flags

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--hostname` | `-u` | GitHub hostname | `github.com` |
| `--enterprise` | `-e` | Enterprise slug (required for `--all-orgs`) | `github` |
| `--org` | `-o` | Target a single organization | - |
| `--all-orgs` | `-a` | Target all organizations in enterprise | - |
| `--orgs-csv` | `-c` | Path to CSV file with organization names | - |
| `--role-name` | `-n` | Custom role name | - |
| `--role-description` | `-d` | Custom role description | - |
| `--base-role` | `-b` | Base role (read, triage, write, maintain) | - |
| `--permissions` | `-p` | Comma-separated permission names | - |
| `--delay` | | Seconds to wait between role creations (mutually exclusive with `--concurrency`) | `0` |
| `--concurrency` | | Number of parallel requests (1-20, mutually exclusive with `--delay`) | `1` |

> [!WARNING]
> **Rate Limiting Considerations**: Setting concurrency higher than 1 increases the likelihood of encountering GitHub's secondary rate limits. To avoid rate limiting issues, consider [exempting the user from rate limits](https://docs.github.com/en/enterprise-server@3.15/admin/administering-your-instance/administering-your-instance-from-the-command-line/command-line-utilities#ghe-config).

### Organization targeting

Choose exactly one of:
- **Single organization**: `--org myorg`
- **All organizations**: `--all-orgs` (requires `--enterprise`)
- **CSV file**: `--orgs-csv organizations.csv`

When no target flag is provided, the extension prompts interactively.

### CSV file format

Create a CSV file with organization names (one per row):

```text
org1
org2
org3
```

## Supported versions

- **GitHub Enterprise Server**: 3.15+
- **GitHub Enterprise Cloud**: Supported

## Limitations

- Base role must be one of: `read`, `triage`, `write`, `maintain`
- Permissions must be valid fine-grained repository permissions for your GitHub instance
- Role names must be unique within each organization
