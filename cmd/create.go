package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

type options struct {
	hostname    string
	enterprise  string
	org         string
	allOrgs     bool
	orgsCSVPath string
	roleName    string
	roleDesc    string
	baseRole    string
	permissions string
	delay       int
	concurrency int
}

type fineGrainedPermission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type customRole struct {
	Name string `json:"name"`
}

type customRolesResponse struct {
	TotalCount int          `json:"total_count"`
	Custom     []customRole `json:"custom_roles"`
}

type metaResponse struct {
	InstalledVersion string `json:"installed_version"`
}

var opts options

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create custom repository roles in GitHub organizations",
	RunE:  runCreate,
}

func init() {
	// Create command flags
	createCmd.Flags().StringVarP(&opts.roleName, "role-name", "n", "", "Custom role name")
	createCmd.Flags().StringVarP(&opts.roleDesc, "role-description", "d", "", "Custom role description")
	createCmd.Flags().StringVarP(&opts.baseRole, "base-role", "b", "", "Base role (read, triage, write, maintain)")
	createCmd.Flags().StringVarP(&opts.permissions, "permissions", "p", "", "Comma-separated list of permission names")
}

func runCreate(_ *cobra.Command, _ []string) error {
	var err error
	if opts.hostname == "" {
		input := pterm.DefaultInteractiveTextInput
		opts.hostname, err = input.Show("GitHub hostname (press enter for github.com)")
		if err != nil {
			return err
		}
		opts.hostname = strings.TrimSpace(opts.hostname)
		if opts.hostname == "" {
			opts.hostname = "github.com"
		}
	}

	if opts.org == "" && !opts.allOrgs && opts.orgsCSVPath == "" {
		selectInput := pterm.DefaultInteractiveSelect.WithOptions([]string{"Single organization", "All organizations in enterprise", "CSV file"})
		mode, modeErr := selectInput.Show("Select target organizations")
		if modeErr != nil {
			return modeErr
		}
		switch mode {
		case "Single organization":
			input := pterm.DefaultInteractiveTextInput
			opts.org, err = input.Show("Organization name")
			if err != nil {
				return err
			}
			opts.org = normalizeOrg(opts.org)
			if opts.org == "" {
				return errors.New("organization name is required")
			}
		case "All organizations in enterprise":
			opts.allOrgs = true
		case "CSV file":
			input := pterm.DefaultInteractiveTextInput
			opts.orgsCSVPath, err = input.Show("Path to CSV file")
			if err != nil {
				return err
			}
			opts.orgsCSVPath = strings.TrimSpace(opts.orgsCSVPath)
			if opts.orgsCSVPath == "" {
				return errors.New("CSV file path is required")
			}
		default:
			return errors.New("invalid target selection")
		}
	}

	// Validate GitHub environment (GHES version and OAuth scopes)
	if err := validateGitHubEnvironment(opts.hostname, opts.allOrgs); err != nil {
		return err
	}

	// Only prompt for enterprise slug if targeting all organizations
	if opts.allOrgs {
		if opts.enterprise == "" {
			input := pterm.DefaultInteractiveTextInput
			opts.enterprise, err = input.Show("GitHub enterprise slug (press enter for github)")
			if err != nil {
				return err
			}
			opts.enterprise = strings.TrimSpace(opts.enterprise)
			if opts.enterprise == "" {
				opts.enterprise = "github"
			}
		}
	} else {
		// Clear enterprise if not targeting all orgs
		opts.enterprise = ""
	}

	orgs, err := resolveOrganizations(opts)
	if err != nil {
		return err
	}
	if len(orgs) == 0 {
		return errors.New("no organizations provided")
	}

	validOrgs := orgs

	if opts.roleName == "" {
		input := pterm.DefaultInteractiveTextInput
		opts.roleName, err = input.Show("Custom role name")
		if err != nil {
			return err
		}
		opts.roleName = strings.TrimSpace(opts.roleName)
	}
	if strings.TrimSpace(opts.roleName) == "" {
		return errors.New("role name is required")
	}

	if opts.roleDesc == "" {
		input := pterm.DefaultInteractiveTextInput
		opts.roleDesc, err = input.Show("Role description (optional)")
		if err != nil {
			return err
		}
	}

	baseRole, err := resolveBaseRole(opts.baseRole)
	if err != nil {
		return err
	}

	permissions, err := listFineGrainedPermissions(opts.hostname, validOrgs[0])
	if err != nil {
		return err
	}
	selectedPermissions, err := resolvePermissions(opts.permissions, permissions)
	if err != nil {
		return err
	}

	// Display confirmation before creating roles
	pterm.Println()
	pterm.DefaultSection.Println("Confirmation")
	pterm.Info.Printfln("Role Name: %s", opts.roleName)
	if opts.roleDesc != "" {
		pterm.Info.Printfln("Description: %s", opts.roleDesc)
	}
	pterm.Info.Printfln("Base Role: %s", baseRole)
	pterm.Info.Printfln("Permissions: %s", strings.Join(selectedPermissions, ", "))
	pterm.Info.Printfln("Target Organizations: %d", len(validOrgs))
	pterm.Println()

	// Validate concurrency bounds
	if opts.concurrency < 1 || opts.concurrency > 20 {
		return fmt.Errorf("concurrency must be between 1 and 20 (got %d)", opts.concurrency)
	}

	// Validate delay is non-negative
	if opts.delay < 0 {
		return fmt.Errorf("delay must be non-negative (got %d)", opts.delay)
	}

	confirm, err := pterm.DefaultInteractiveConfirm.Show("Begin role creation?")
	if err != nil {
		return err
	}
	if !confirm {
		pterm.Info.Println("Role creation cancelled.")
		return nil
	}
	pterm.Println()

	progressBar, err := pterm.DefaultProgressbar.WithTotal(len(validOrgs)).WithTitle("Creating custom roles").Start()
	if err != nil {
		return err
	}
	defer progressBar.Stop()

	successCount := 0
	warningCount := 0
	errorCount := 0

	// If delay is set, use sequential processing with delays
	if opts.delay > 0 {
		for i, org := range validOrgs {
			exists, existsErr := roleExists(opts.hostname, org, opts.roleName)
			if existsErr != nil {
				// Check if it's a 404 (org not found)
				if isNotFoundError(existsErr) {
					pterm.Warning.Printfln("Organization %s not found. Skipping.", org)
					warningCount++
					progressBar.Increment()
					continue
				}
				pterm.Error.Printfln("Failed to check existing roles for %s: %v", org, existsErr)
				errorCount++
				progressBar.Increment()
				continue
			}
			if exists {
				pterm.Warning.Printfln("Organization %s already has a role named %s. Skipping.", org, opts.roleName)
				warningCount++
				progressBar.Increment()
				continue
			}

			createErr := createCustomRole(opts.hostname, org, opts.roleName, opts.roleDesc, baseRole, selectedPermissions)
			if createErr != nil {
				// Check if it's a 404 (org not found)
				if isNotFoundError(createErr) {
					pterm.Warning.Printfln("Organization %s not found. Skipping.", org)
					warningCount++
					progressBar.Increment()
					continue
				}
				pterm.Error.Printfln("Failed to create role in %s: %v", org, createErr)
				errorCount++
				progressBar.Increment()
				continue
			}
			pterm.Success.Printfln("Created role %s in %s", opts.roleName, org)
			successCount++
			progressBar.Increment()

			// Add delay between requests (except after the last one)
			if i < len(validOrgs)-1 {
				time.Sleep(time.Duration(opts.delay) * time.Second)
			}
		}
	} else {
		// Use concurrent processing with semaphore
		var wg sync.WaitGroup
		var mu sync.Mutex
		semaphore := make(chan struct{}, opts.concurrency)

		for _, org := range validOrgs {
			wg.Add(1)
			semaphore <- struct{}{} // Acquire semaphore

			go func(org string) {
				defer wg.Done()
				defer func() { <-semaphore }() // Release semaphore

				exists, existsErr := roleExists(opts.hostname, org, opts.roleName)
				if existsErr != nil {
					mu.Lock()
					if isNotFoundError(existsErr) {
						pterm.Warning.Printfln("Organization %s not found. Skipping.", org)
						warningCount++
					} else {
						pterm.Error.Printfln("Failed to check existing roles for %s: %v", org, existsErr)
						errorCount++
					}
					progressBar.Increment()
					mu.Unlock()
					return
				}
				if exists {
					mu.Lock()
					pterm.Warning.Printfln("Organization %s already has a role named %s. Skipping.", org, opts.roleName)
					warningCount++
					progressBar.Increment()
					mu.Unlock()
					return
				}

				createErr := createCustomRole(opts.hostname, org, opts.roleName, opts.roleDesc, baseRole, selectedPermissions)
				mu.Lock()
				if createErr != nil {
					if isNotFoundError(createErr) {
						pterm.Warning.Printfln("Organization %s not found. Skipping.", org)
						warningCount++
					} else {
						pterm.Error.Printfln("Failed to create role in %s: %v", org, createErr)
						errorCount++
					}
				} else {
					pterm.Success.Printfln("Created role %s in %s", opts.roleName, org)
					successCount++
				}
				progressBar.Increment()
				mu.Unlock()
			}(org)
		}

		wg.Wait()
	}

	progressBar.Stop()

	// Display summary
	pterm.Println()
	pterm.DefaultSection.Println("Summary")
	pterm.Info.Printfln("✓ Successfully created: %d", successCount)
	if warningCount > 0 {
		pterm.Warning.Printfln("⚠ Warnings: %d", warningCount)
	}
	if errorCount > 0 {
		pterm.Error.Printfln("✗ Errors: %d", errorCount)
	}

	// Display command for replication
	pterm.Println()
	pterm.FgMagenta.Println("💡 Tip: To replicate these changes without the interactive process, use:")
	pterm.Println()
	cmd := buildReplicationCommand(opts, baseRole, selectedPermissions)
	pterm.Println(cmd)
	pterm.Println()

	if errorCount > 0 {
		return fmt.Errorf("completed with %d errors", errorCount)
	}
	return nil
}

func resolveOrganizations(opts options) ([]string, error) {
	if opts.allOrgs {
		return fetchOrganizations(opts.hostname, opts.enterprise)
	}
	if opts.org != "" {
		return []string{normalizeOrg(opts.org)}, nil
	}
	if opts.orgsCSVPath != "" {
		return loadOrganizationsFromCSV(opts.orgsCSVPath)
	}
	return nil, errors.New("no organization target specified")
}

func normalizeOrg(org string) string {
	return strings.ToLower(strings.TrimSpace(org))
}

func loadOrganizationsFromCSV(path string) ([]string, error) {
	cleanPath := filepath.Clean(path)
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	orgSet := map[string]bool{}
	var orgs []string
	for _, record := range records {
		for _, value := range record {
			org := normalizeOrg(value)
			if org == "" || orgSet[org] {
				continue
			}
			orgSet[org] = true
			orgs = append(orgs, org)
		}
	}
	return orgs, nil
}

func resolveBaseRole(baseRole string) (string, error) {
	options := []string{"read", "triage", "write", "maintain"}
	if baseRole != "" {
		baseRole = strings.ToLower(strings.TrimSpace(baseRole))
		for _, option := range options {
			if baseRole == option {
				return baseRole, nil
			}
		}
		return "", fmt.Errorf("invalid base role: %s", baseRole)
	}

	selectInput := pterm.DefaultInteractiveSelect.WithOptions(options)
	choice, err := selectInput.Show("Select base role")
	if err != nil {
		return "", err
	}
	return choice, nil
}

func resolvePermissions(flagValue string, permissions []fineGrainedPermission) ([]string, error) {
	if len(permissions) == 0 {
		return nil, errors.New("no permissions available for this organization")
	}

	permissionMap := map[string]bool{}
	for _, perm := range permissions {
		permissionMap[perm.Name] = true
	}

	if strings.TrimSpace(flagValue) != "" {
		items := strings.Split(flagValue, ",")
		var selected []string
		for _, item := range items {
			name := strings.TrimSpace(item)
			if name == "" {
				continue
			}
			if !permissionMap[name] {
				return nil, fmt.Errorf("unknown permission: %s", name)
			}
			selected = append(selected, name)
		}
		if len(selected) == 0 {
			return nil, errors.New("permissions are required")
		}
		return uniqueStrings(selected), nil
	}

	sort.SliceStable(permissions, func(i, j int) bool {
		return permissions[i].Name < permissions[j].Name
	})

	options := make([]string, 0, len(permissions))
	lookup := map[string]string{}
	for _, perm := range permissions {
		label := perm.Name
		if perm.Description != "" {
			label = fmt.Sprintf("%s — %s", perm.Name, perm.Description)
		}
		options = append(options, label)
		lookup[label] = perm.Name
	}

	selection, err := pterm.DefaultInteractiveMultiselect.
		WithOptions(options).
		WithFilter(true).
		WithMaxHeight(10).
		Show("Select permissions (Type to filter, ↑↓ to navigate, Enter to toggle, Tab to confirm)")
	if err != nil {
		return nil, err
	}
	if len(selection) == 0 {
		return nil, errors.New("permissions are required")
	}

	var selected []string
	for _, label := range selection {
		selected = append(selected, lookup[label])
	}
	return uniqueStrings(selected), nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func roleExists(hostname, org, roleName string) (bool, error) {
	response, stderr, err := ghAPI(hostname, "orgs/"+org+"/custom-repository-roles")
	if err != nil {
		return false, fmt.Errorf("custom role lookup failed: %w (%s)", err, stderr.String())
	}

	var payload customRolesResponse
	if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
		return false, err
	}

	roleName = strings.ToLower(strings.TrimSpace(roleName))
	for _, role := range payload.Custom {
		if strings.ToLower(role.Name) == roleName {
			return true, nil
		}
	}
	return false, nil
}

func createCustomRole(hostname, org, name, description, baseRole string, permissions []string) error {
	args := []string{
		"-X", "POST",
		"orgs/" + org + "/custom-repository-roles",
		"-f", "name=" + name,
		"-f", "base_role=" + baseRole,
	}
	if strings.TrimSpace(description) != "" {
		args = append(args, "-f", "description="+description)
	}
	for _, permission := range permissions {
		args = append(args, "-f", "permissions[]="+permission)
	}
	_, stderr, err := ghAPI(hostname, args...)
	if err != nil {
		return fmt.Errorf("create role failed: %w (%s)", err, stderr.String())
	}
	return nil
}

func listFineGrainedPermissions(hostname, org string) ([]fineGrainedPermission, error) {
	response, stderr, err := ghAPI(hostname, "orgs/"+org+"/repository-fine-grained-permissions")
	if err != nil {
		return nil, fmt.Errorf("permissions lookup failed: %w (%s)", err, stderr.String())
	}

	var permissions []fineGrainedPermission
	if err := json.Unmarshal(response.Bytes(), &permissions); err != nil {
		return nil, err
	}
	return permissions, nil
}

func fetchOrganizations(hostname, enterprise string) ([]string, error) {
	if enterprise == "" {
		return nil, fmt.Errorf("--enterprise flag is required")
	}

	pterm.Info.Println("Fetching organizations for enterprise...")

	var spinner *pterm.SpinnerPrinter
	stopSpinner := func() {
		if spinner != nil {
			spinner.Stop()
		}
	}
	defer stopSpinner()

	const maxPerPage = 100
	var orgs []string
	var cursor *string

	for {
		query := `{
			enterprise(slug: "` + enterprise + `") {
				organizations(first: ` + fmt.Sprintf("%d", maxPerPage) + `, after: ` + formatCursor(cursor) + `) {
					nodes {
						login
					}
					pageInfo {
						hasNextPage
						endCursor
					}
				}
			}
		}`

		response, stderr, execErr := gh.Exec("api", "--hostname", hostname, "graphql", "-f", "query="+query)
		if execErr != nil {
			pterm.Error.Printf("Failed to fetch organizations for enterprise '%s': %v\n", enterprise, execErr)
			pterm.Error.Printf("GraphQL query: %s\n", query)
			pterm.Error.Printf("gh CLI stderr: %s\n", stderr.String())
			return nil, execErr
		}

		var result struct {
			Data struct {
				Enterprise struct {
					Organizations struct {
						Nodes []struct {
							Login string `json:"login"`
						}
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"organizations"`
				} `json:"enterprise"`
			} `json:"data"`
		}

		if err := json.Unmarshal(response.Bytes(), &result); err != nil {
			pterm.Error.Printf("Failed to parse organizations data for enterprise '%s': %v\n", enterprise, err)
			return nil, err
		}

		for _, org := range result.Data.Enterprise.Organizations.Nodes {
			orgs = append(orgs, normalizeOrg(org.Login))
		}

		// Start spinner only after we have successfully fetched at least one page.
		if spinner == nil {
			started, err := pterm.DefaultSpinner.Start(fmt.Sprintf("Fetched %d organizations", len(orgs)))
			if err != nil {
				return nil, err
			}
			spinner = started
		} else {
			spinner.UpdateText(fmt.Sprintf("Fetched %d organizations", len(orgs)))
		}

		if !result.Data.Enterprise.Organizations.PageInfo.HasNextPage {
			break
		}
		cursor = &result.Data.Enterprise.Organizations.PageInfo.EndCursor
	}

	return uniqueStrings(orgs), nil
}

func formatCursor(cursor *string) string {
	if cursor == nil || *cursor == "" {
		return "null"
	}
	return `"` + *cursor + `"`
}

func ghAPI(hostname string, args ...string) (bytes.Buffer, bytes.Buffer, error) {
	fullArgs := []string{"api", "--hostname", hostname, "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28"}
	fullArgs = append(fullArgs, args...)
	return gh.Exec(fullArgs...)
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errorText := strings.ToLower(err.Error())
	return strings.Contains(errorText, "404") || strings.Contains(errorText, "not found")
}

func buildReplicationCommand(opts options, baseRole string, permissions []string) string {
	cmd := "gh custom-roles create"

	if opts.hostname != "" {
		cmd += " --hostname " + opts.hostname
	}
	if opts.enterprise != "" {
		cmd += " --enterprise " + opts.enterprise
	}
	if opts.org != "" {
		cmd += " --org " + opts.org
	} else if opts.allOrgs {
		cmd += " --all-orgs"
	} else if opts.orgsCSVPath != "" {
		cmd += " --orgs-csv " + opts.orgsCSVPath
	}
	if opts.roleName != "" {
		cmd += " --role-name '" + opts.roleName + "'"
	}
	if opts.roleDesc != "" {
		cmd += " --role-description '" + opts.roleDesc + "'"
	}
	if baseRole != "" {
		cmd += " --base-role " + baseRole
	}
	if len(permissions) > 0 {
		permStr := strings.Join(permissions, ",")
		cmd += " --permissions " + permStr
	}
	if opts.delay > 0 {
		cmd += fmt.Sprintf(" --delay %d", opts.delay)
	}
	if opts.concurrency > 1 {
		cmd += fmt.Sprintf(" --concurrency %d", opts.concurrency)
	}

	return cmd
}

// validateGitHubEnvironment validates GHES version and OAuth scopes
func validateGitHubEnvironment(hostname string, targetingAllOrgs bool) error {
	client, err := api.NewRESTClient(api.ClientOptions{
		Host: hostname,
		Headers: map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": "2022-11-28",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to initialize GitHub API client: %w", err)
	}

	resp, err := client.Request("GET", "meta", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch GitHub meta endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("failed to fetch GitHub meta endpoint: %s (%s)", resp.Status, strings.TrimSpace(string(snippet)))
	}

	oauthScopes := resp.Header.Get("X-OAuth-Scopes")

	var meta metaResponse
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return fmt.Errorf("failed to parse GitHub meta response: %w", err)
	}
	installedVersion := meta.InstalledVersion

	// Validate GHES version
	if installedVersion != "" {
		// Parse version and check if it's below 3.15
		version := strings.TrimSpace(installedVersion)
		if isVersionBelow(version, "3.15.0") {
			pterm.Warning.Printfln("Warning: GitHub Enterprise Server version %s is below 3.15.0. Some features may not be supported.", version)
		}
	}

	// Validate OAuth scopes
	scopes := parseOAuthScopes(oauthScopes)

	// Check for admin:org scope (required for all operations)
	if !hasScope(scopes, "admin:org") {
		return fmt.Errorf("missing required OAuth scope 'admin:org'. Please run: gh auth refresh -h %s -s admin:org", hostname)
	}

	// Check for read:enterprise scope when targeting all orgs
	if targetingAllOrgs && !hasScope(scopes, "read:enterprise") {
		return fmt.Errorf("missing required OAuth scope 'read:enterprise' for targeting all organizations. Please run: gh auth refresh -h %s -s read:enterprise", hostname)
	}

	return nil
}

// parseOAuthScopes parses comma-separated OAuth scopes
func parseOAuthScopes(scopesHeader string) []string {
	if scopesHeader == "" {
		return []string{}
	}
	parts := strings.Split(scopesHeader, ",")
	var scopes []string
	for _, part := range parts {
		scope := strings.TrimSpace(part)
		if scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

// hasScope checks if a scope exists in the list of scopes
func hasScope(scopes []string, targetScope string) bool {
	for _, scope := range scopes {
		if scope == targetScope {
			return true
		}
	}
	return false
}

// isVersionBelow checks if version1 is below version2
func isVersionBelow(version1, version2 string) bool {
	v1Parts := parseVersion(version1)
	v2Parts := parseVersion(version2)

	for i := 0; i < 3; i++ {
		if v1Parts[i] < v2Parts[i] {
			return true
		}
		if v1Parts[i] > v2Parts[i] {
			return false
		}
	}
	return false
}

// parseVersion parses a version string (e.g., "3.15.0") into [major, minor, patch]
func parseVersion(version string) [3]int {
	var parts [3]int
	components := strings.Split(version, ".")
	for i := 0; i < 3 && i < len(components); i++ {
		var val int
		// Scan only the numeric prefix to handle versions like "3.15.0-beta"
		_, err := fmt.Sscanf(components[i], "%d", &val)
		if err != nil {
			// If parsing fails, leave as 0
			val = 0
		}
		parts[i] = val
	}
	return parts
}
