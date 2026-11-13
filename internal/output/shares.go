package output

import (
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

const (
	shareBase        = "https://volcano.dev/deploy/"
	deployButtonURL  = "https://volcano.dev/deploy-button.svg"
	deployButtonAlt  = "Deploy with Volcano"
	statsDailyMaxRow = 14
)

// shareKinds is the order stats show kinds in: the path a visitor takes
// from seeing the link to a finished deploy, then the failure.
var shareKinds = []apiclient.ShareKindCountKind{
	apiclient.ShareKindCountKindPreview,
	apiclient.ShareKindCountKindClick,
	apiclient.ShareKindCountKindConfirm,
	apiclient.ShareKindCountKindAccepted,
	apiclient.ShareKindCountKindReady,
	apiclient.ShareKindCountKindFailed,
}

// ShareURL returns the public address of a share.
func ShareURL(id string) string {
	return shareBase + url.PathEscape(id)
}

// ShareMarkdownSnippet returns the Markdown that shows the deploy button
// for a link.
func ShareMarkdownSnippet(id string) string {
	return fmt.Sprintf("[![%s](%s)](%s)", deployButtonAlt, deployButtonURL, ShareURL(id))
}

// ShareHTMLSnippet returns the HTML that shows the deploy button for a
// link.
func ShareHTMLSnippet(id string) string {
	return fmt.Sprintf("<a href=%q><img src=%q alt=%q></a>", ShareURL(id), deployButtonURL, deployButtonAlt)
}

// ShareStatus renders how a project is shared: whether sharing is on and its
// link serves, its link, and the release people fork.
func ShareStatus(w io.Writer, share *apiclient.Share, release *apiclient.ShareRelease) {
	if share == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Sharing", "%s", SharingState(share))
	kv(w, on, "Link", "%s", ShareURL(theme.StripControl(share.Id)))
	description := "none"
	if share.Description != "" {
		description = theme.StripControl(share.Description)
	}
	kv(w, on, "Description", "%s", description)
	reshare := "no"
	if share.AllowReshare {
		reshare = "yes"
	}
	kv(w, on, "Copies shareable", "%s", reshare)
	switch {
	case release != nil:
		kv(w, on, "Shared release", "%s (published %s)", release.Id.String(), FormatTimestamp(release.PublishedAt))
		kv(w, on, "Contents", "%d function(s), %d frontend(s), %d variable(s)",
			len(release.Functions), len(release.Frontends), len(release.Variables))
	case share.CurrentRelease != nil:
		kv(w, on, "Shared release", "%s (published %s)", share.CurrentRelease.Id.String(), FormatTimestamp(share.CurrentRelease.PublishedAt))
	default:
		kv(w, on, "Shared release", "none yet: nothing is offered until you run volcano share")
	}
}

// The reasons a share is off that the CLI words. The set is open: any other
// reason reads as off, with the reason.
const (
	disabledByOwner    = "owner"
	disabledByOperator = "operator"
)

// SharingState says in words whether a share is on and its link serves the
// release: an active share can still serve nothing, and one Volcano took down
// stays off until Volcano lifts it.
func SharingState(share *apiclient.Share) string {
	if share.Status != apiclient.ShareStatusActive {
		reason := ""
		if share.DisabledReason != nil {
			reason = theme.StripControl(strings.TrimSpace(*share.DisabledReason))
		}
		switch reason {
		case disabledByOperator:
			return "off: Volcano took this share down, and only Volcano can switch it on again. " +
				"Contact support if you think this is a mistake"
		case disabledByOwner:
			return "off (you switched it off)"
		case "":
			return "off"
		default:
			return "off (" + reason + ")"
		}
	}
	switch {
	case share.Serving:
		return "on"
	case share.CurrentRelease == nil:
		return "on, but nothing is published yet, so the link answers like one switched off"
	default:
		return "on, but the link is not serving right now: sharing is not available here, the project is " +
			"being deleted, your account does not allow traffic, or the project is a copy that may no " +
			"longer be shared"
	}
}

// ShareSnippets renders the Markdown and HTML that show a deploy button
// for a link, to paste into a README or a page.
func ShareSnippets(w io.Writer, linkID string) {
	on := theme.On(w)
	id := theme.StripControl(linkID)
	fmt.Fprintf(w, "\n%s\n  %s\n", theme.Title("Markdown", on), ShareMarkdownSnippet(id))
	fmt.Fprintf(w, "\n%s\n  %s\n", theme.Title("HTML", on), ShareHTMLSnippet(id))
}

// ShareReleaseDraft renders what sharing would publish: the functions and
// frontends, the variables every copy gets, empty, the functions that would
// receive each copy's service key, and what is left out.
func ShareReleaseDraft(w io.Writer, draft *apiclient.ShareReleaseDraft) {
	if draft == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Functions", "%s", releaseFunctions(draft.Functions))
	kv(w, on, "Frontends", "%s", nameList(draft.Frontends))
	kv(w, on, "Variables", "%s", variableList(draft.Variables))
	kv(w, on, "Service key", "%s", nameList(draft.ServiceKeyFunctions))
	omissions(w, on, draft.Omissions)
}

// ShareReleases renders a share's releases, newest first.
func ShareReleases(w io.Writer, releases []apiclient.ShareRelease) {
	if len(releases) == 0 {
		fmt.Fprintln(w, "No releases published yet")
		return
	}
	on := theme.On(w)
	tableHead(w, on, true, 90, "%-36s  %-16s  %9s  %9s  %s", "ID", "Published", "Functions", "Frontends", "Variables")
	for i := range releases {
		release := &releases[i]
		fmt.Fprintf(w, "%-36s  %-16s  %9d  %9d  %s\n",
			release.Id.String(),
			Truncate(FormatTimeAgo(release.PublishedAt), 16),
			len(release.Functions),
			len(release.Frontends),
			variableNames(release.Variables),
		)
	}
	summary(w, on, "Showing %d release(s)", len(releases))
}

// ShareStats renders a share's click and fork counts: totals per kind,
// then the most recent days.
func ShareStats(w io.Writer, linkID string, stats *apiclient.ShareStats) {
	if stats == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Link", "%s", theme.StripControl(linkID))
	kv(w, on, "Since", "%s", FormatTimestamp(stats.Since))

	if len(stats.Totals) == 0 && len(stats.Daily) == 0 {
		fmt.Fprintln(w, "\nNo activity in this window")
		return
	}

	totals := map[apiclient.ShareKindCountKind]apiclient.ShareKindCount{}
	for _, total := range stats.Totals {
		totals[total.Kind] = total
	}
	tableHead(w, on, true, 36, "%-12s  %8s  %8s", "Kind", "Events", "Visitors")
	for _, kind := range shareKinds {
		total, ok := totals[kind]
		if !ok {
			continue
		}
		fmt.Fprintf(w, "%-12s  %8d  %8d\n", kind, total.Events, total.Visitors)
	}

	if len(stats.Daily) > 0 {
		shareDaily(w, on, stats.Daily)
	}
}

// shareDaily prints one row per day, newest first, with a column for each
// kind. Days with no events are absent from the API's series, so they are
// absent here too.
func shareDaily(w io.Writer, on bool, daily []apiclient.ShareDailyCount) {
	type dayRow struct {
		day    string
		counts map[apiclient.ShareDailyCountKind]int64
	}
	rows := map[string]*dayRow{}
	for _, entry := range daily {
		day := entry.Day.Format("2006-01-02")
		row, ok := rows[day]
		if !ok {
			row = &dayRow{day: day, counts: map[apiclient.ShareDailyCountKind]int64{}}
			rows[day] = row
		}
		row.counts[entry.Kind] += entry.Events
	}
	days := make([]string, 0, len(rows))
	for day := range rows {
		days = append(days, day)
	}
	slices.Sort(days)
	slices.Reverse(days)
	shown := days
	if len(shown) > statsDailyMaxRow {
		shown = shown[:statsDailyMaxRow]
	}

	header := []any{"Day"}
	for _, kind := range shareKinds {
		header = append(header, string(kind))
	}
	tableHead(w, on, true, 10+len(shareKinds)*10, "%-10s"+strings.Repeat("  %8s", len(shareKinds)), header...)
	for _, day := range shown {
		cells := []any{day}
		for _, kind := range shareKinds {
			cells = append(cells, rows[day].counts[apiclient.ShareDailyCountKind(kind)])
		}
		fmt.Fprintf(w, "%-10s"+strings.Repeat("  %8d", len(shareKinds))+"\n", cells...)
	}
	if len(days) > len(shown) {
		summary(w, on, "Showing the latest %d of %d days; --json has every day", len(shown), len(days))
	}
}

// SharePreview renders the release a share offers a visitor: when
// it was published, what installing it creates, what it asks for, and which
// functions receive the new project's service key.
func SharePreview(w io.Writer, preview *apiclient.SharePreview) {
	if preview == nil {
		return
	}
	on := theme.On(w)
	release := preview.Release
	kv(w, on, "Share", "%s", ShareURL(theme.StripControl(preview.Id)))
	kv(w, on, "Release", "%s (published %s)", release.Id.String(), FormatTimestamp(release.PublishedAt))
	kv(w, on, "Digest", "sha256:%s", theme.StripControl(release.Sha256))
	if preview.Description != "" {
		kv(w, on, "Description", "%s", theme.StripControl(preview.Description))
	}
	kv(w, on, "Frontends", "%s", nameList(release.Frontends))
	kv(w, on, "Functions", "%s", nameList(release.Functions))
	kv(w, on, "Variables", "%s", variableList(release.Variables))
	omissions(w, on, release.Omissions)
	if len(release.ServiceKeyFunctions) > 0 {
		fmt.Fprintf(w, "\n%s\n", theme.Warn(fmt.Sprintf(
			"These functions receive your new project's service key, which bypasses row-level security: %s.",
			nameList(release.ServiceKeyFunctions)), on))
	}
	fmt.Fprintf(w, "\n%s\n", theme.Warn("Volcano has not reviewed this code. Fork only projects you trust.", on))
}

// ShareReleaseChanges says how the release a share offers now differs from
// the one a visitor reviewed. It lists only what changed.
func ShareReleaseChanges(w io.Writer, before, after *apiclient.SharePreviewRelease) {
	if before == nil || after == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Release", "%s (published %s), was %s", after.Id.String(), FormatTimestamp(after.PublishedAt), before.Id.String())
	changedNames(w, on, "Frontends", before.Frontends, after.Frontends)
	changedNames(w, on, "Functions", before.Functions, after.Functions)
	changedNames(w, on, "Service key", before.ServiceKeyFunctions, after.ServiceKeyFunctions)
	changedNames(w, on, "Variables", variableLabels(before.Variables), variableLabels(after.Variables))
}

func changedNames(w io.Writer, on bool, label string, before, after []string) {
	var added, removed []string
	for _, name := range after {
		if !slices.Contains(before, name) {
			added = append(added, "+"+theme.StripControl(name))
		}
	}
	for _, name := range before {
		if !slices.Contains(after, name) {
			removed = append(removed, "-"+theme.StripControl(name))
		}
	}
	added = append(added, removed...)
	if len(added) > 0 {
		kv(w, on, label, "%s", strings.Join(added, ", "))
	}
}

// TemplateSource names the share release a project was installed from.
func TemplateSource(source *apiclient.ProjectTemplateInstallationSource) string {
	return "share " + theme.StripControl(source.ShareId) + ", release " + source.ReleaseId.String()
}

// installationFailures words the failure codes an installation reports. The
// set is open, so InstallationFailure falls back for a code not listed here.
var installationFailures = map[string]string{
	"source_withdrawn":   "the share was switched off or deleted, or its project is being deleted",
	"source_invalid":     "the release failed Volcano's checks, such as an archive whose digest, size or contents are wrong",
	"source_access_lost": "Volcano can no longer read the source",
	"invalid_source_configuration": "the source's volcano-config.yaml is invalid, or it declares a variable " +
		"the new project does not have",
	"nothing_to_deploy":               "the source has no function or frontend",
	"database_not_supported":          "the source ships database SQL, which is not supported",
	"function_deploy_refused":         "a function in the source cannot be deployed as defined",
	"function_quota_exceeded":         "the project reached its plan's function limit",
	"durable_function_quota_exceeded": "the project reached its plan's durable function limit",
	"frontend_quota_exceeded":         "the project reached its plan's frontend limit",
	"frontend_deploy_limit":           "the project reached its plan's frontend deploy limit",
	"build_minutes_exceeded":          "the account used up its plan's build minutes",
	"build_failed":                    "a function or frontend from the release failed to build or deploy; its deployment's build logs say why",
	"retry_budget_exhausted":          "the installation kept failing and Volcano stopped retrying",
	"internal_error":                  "Volcano failed for a reason of its own",
}

// InstallationFailure says in words why an installation failed, from its
// failure code. A code this CLI does not know reads as a generic failure with
// the code itself, and an absent one as a failure with no reason recorded.
func InstallationFailure(code *string) string {
	if code == nil || strings.TrimSpace(*code) == "" {
		return "no reason was recorded"
	}
	cleaned := theme.StripControl(strings.TrimSpace(*code))
	if words, ok := installationFailures[cleaned]; ok {
		return words
	}
	return "it failed (" + cleaned + ")"
}

func releaseFunctions(functions []apiclient.ShareReleaseFunction) string {
	if len(functions) == 0 {
		return "none"
	}
	described := make([]string, len(functions))
	for i, function := range functions {
		details := []string{string(function.Kind)}
		if function.Visibility != nil && *function.Visibility != "" {
			details = append(details, theme.StripControl(*function.Visibility))
		}
		if function.InvocationMode != nil && *function.InvocationMode != "" {
			details = append(details, theme.StripControl(*function.InvocationMode))
		}
		described[i] = fmt.Sprintf("%s (%s)", theme.StripControl(function.Name), strings.Join(details, ", "))
	}
	return strings.Join(described, ", ")
}

// variableLabels names each variable with what a visitor must know about it:
// a copy gets it empty unless a value is given, and whether it is a secret.
func variableLabels(variables []apiclient.ShareVariable) []string {
	labels := make([]string, len(variables))
	for i, variable := range variables {
		details := []string{"starts empty"}
		if variable.Secret {
			details = append(details, "secret")
		}
		labels[i] = fmt.Sprintf("%s (%s)", theme.StripControl(variable.Name), strings.Join(details, ", "))
	}
	return labels
}

// EmptyVariables lists the variables a new copy has with no value, and how to
// set them. It prints nothing when every variable was given a value.
func EmptyVariables(w io.Writer, names []string, setHint string) {
	if len(names) == 0 {
		return
	}
	on := theme.On(w)
	cleaned := make([]string, len(names))
	for i, name := range names {
		cleaned[i] = theme.StripControl(name)
	}
	fmt.Fprintf(w, "\n%s\n", theme.Warn("These variables start empty, so code that reads them may not work until you set them: "+
		strings.Join(cleaned, ", "), on))
	fmt.Fprintln(w, setHint)
}

func variableList(variables []apiclient.ShareVariable) string {
	if len(variables) == 0 {
		return "none"
	}
	return strings.Join(variableLabels(variables), ", ")
}

func variableNames(variables []apiclient.ShareVariable) string {
	names := make([]string, len(variables))
	for i, variable := range variables {
		names[i] = variable.Name
	}
	return nameList(names)
}

// omissions lists what a release does not carry, one line each, so an owner
// sees what visitors go without and a visitor sees, for one, that the project
// starts with no database.
func omissions(w io.Writer, on bool, omitted []apiclient.ShareReleaseOmission) {
	if len(omitted) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n", theme.Title("Left out", on))
	for _, omission := range omitted {
		fmt.Fprintf(w, "  - %s\n", omissionText(omission))
	}
}

func omissionText(omission apiclient.ShareReleaseOmission) string {
	resource, path := "", ""
	if omission.Resource != nil {
		resource = theme.StripControl(*omission.Resource)
	}
	if omission.Path != nil {
		path = theme.StripControl(*omission.Path)
	}
	switch omission.Kind {
	case apiclient.ShareReleaseOmissionKindDatabase:
		return fmt.Sprintf("database %s: the new project starts without it", resource)
	case apiclient.ShareReleaseOmissionKindSandbox:
		return fmt.Sprintf("sandbox %s: releases do not deploy sandboxes", resource)
	case apiclient.ShareReleaseOmissionKindNoSource:
		return resource + ": its current deployment has no stored source"
	case apiclient.ShareReleaseOmissionKindSecretFile:
		return omissionPlace(resource, path) + ": dotenv and key files are never released"
	case apiclient.ShareReleaseOmissionKindUnsupportedEntry:
		return omissionPlace(resource, path) + ": not a regular file"
	default:
		return strings.TrimSpace(fmt.Sprintf("%s %s", theme.StripControl(string(omission.Kind)), omissionPlace(resource, path)))
	}
}

func omissionPlace(resource, path string) string {
	switch {
	case resource != "" && path != "":
		return resource + ": " + path
	case path != "":
		return path
	default:
		return resource
	}
}

func nameList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	cleaned := make([]string, len(names))
	for i, name := range names {
		cleaned[i] = theme.StripControl(name)
	}
	return strings.Join(cleaned, ", ")
}
