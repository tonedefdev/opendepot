package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// resourceDetail is the skill or agent detail returned by the registry. Every key is always written by --json, so
// absent values are null or empty rather than omitted.
type resourceDetail struct {
	Kind                   string                                    `json:"kind"`
	Namespace              string                                    `json:"namespace"`
	Name                   string                                    `json:"name"`
	LatestVersion          string                                    `json:"latestVersion"`
	Synced                 bool                                      `json:"synced"`
	SyncStatus             string                                    `json:"syncStatus"`
	HasUnsyncedVersions    bool                                      `json:"hasUnsyncedVersions"`
	Public                 bool                                      `json:"public"`
	Blocked                bool                                      `json:"blocked"`
	BlockReasons           []string                                  `json:"blockReasons"`
	JevRiskLevel           string                                    `json:"jevRiskLevel"`
	SourceRef              string                                    `json:"sourceRef"`
	SourceCommit           string                                    `json:"sourceCommit"`
	SourcePath             string                                    `json:"sourcePath"`
	RepoURL                string                                    `json:"repoUrl"`
	RepoOwner              string                                    `json:"repoOwner"`
	ScanCounts             *resourceScanCounts                       `json:"scanCounts"`
	LastScanned            string                                    `json:"lastScanned"`
	TotalDownloads         int64                                     `json:"totalDownloads"`
	LastDownloadedAt       string                                    `json:"lastDownloadedAt"`
	DefinitionFileName     string                                    `json:"definitionFileName"`
	DefinitionContent      string                                    `json:"definitionContent"`
	AgentMetadata          *resourceMetadata                         `json:"agentMetadata"`
	Versions               []resourceVersion                         `json:"versions"`
	JevAssessment          *opendepotv1alpha1.JevAssessment          `json:"jevAssessment"`
	JevExemptionGovernance *opendepotv1alpha1.JevExemptionGovernance `json:"jevExemptionGovernance"`
	JevUnblock             *opendepotv1alpha1.JevUnblock             `json:"jevUnblock"`
	JevThresholds          *opendepotv1alpha1.JevPolicy              `json:"jevThresholds"`
}

type resourceMetadata struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	Model       string   `json:"model"`
}

type resourceScanCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
	Exempted int `json:"exempted"`
}

type resourceVersion struct {
	Name              string              `json:"name"`
	Version           string              `json:"version"`
	Commit            string              `json:"commit"`
	Synced            bool                `json:"synced"`
	SyncStatus        string              `json:"syncStatus"`
	BranchPhase       string              `json:"branchPhase"`
	Blocked           bool                `json:"blocked"`
	BlockReasons      []string            `json:"blockReasons"`
	OS                string              `json:"os"`
	Arch              string              `json:"arch"`
	FileName          string              `json:"fileName"`
	Checksum          string              `json:"checksum"`
	ArchiveSizeBytes  int64               `json:"archiveSizeBytes"`
	DownloadCount     int64               `json:"downloadCount"`
	LastDownloadedAt  string              `json:"lastDownloadedAt"`
	LastScanned       string              `json:"lastScanned"`
	ScanCounts        *resourceScanCounts `json:"scanCounts"`
	SchemaState       string              `json:"schemaState"`
	SchemaMessage     string              `json:"schemaMessage"`
	SchemaAttemptedAt string              `json:"schemaAttemptedAt"`
}

func (a *app) describeResource(ctx context.Context, kind entryKind, resource, registry string, asJSON bool) error {
	parts := strings.Split(resource, "/")
	if len(parts) != 2 || len(validation.IsDNS1123Label(parts[0])) != 0 || len(validation.IsDNS1123Subdomain(parts[1])) != 0 {
		return fmt.Errorf("resource %q must be a valid namespace/name", resource)
	}

	proj, err := loadOptionalProject(a.path(configFileName))
	if err != nil {
		return err
	}

	host, err := resolveListRegistry(registry, proj)
	if err != nil {
		return err
	}

	u, err := url.Parse(a.rootURL(host, "/opendepot/ui/v1/resources/"))
	if err != nil {
		return fmt.Errorf("describe %s %s: invalid registry URL: %w", kind.singular(), resource, err)
	}

	body, err := a.get(ctx, u.JoinPath(parts[0], kind.singular(), parts[1]).String(), host)
	if err != nil {
		var status *httpStatusError
		if errors.As(err, &status) {
			switch status.code {
			case 401:
				return fmt.Errorf("describe %s %s: not authorized (HTTP 401); run 'opendepot login %s' and retry", kind.singular(), resource, host)
			case 403:
				return fmt.Errorf("describe %s %s: access denied (HTTP 403)", kind.singular(), resource)
			case 404:
				return fmt.Errorf("%s %s not found or not visible on %s", kind.singular(), resource, host)
			}
		}

		return fmt.Errorf("describe %s %s: %w", kind.singular(), resource, err)
	}

	var detail resourceDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return fmt.Errorf("describe %s %s: decode response: %w", kind.singular(), resource, err)
	}

	if detail.Kind != kind.singular() || detail.Namespace != parts[0] || detail.Name != parts[1] {
		return fmt.Errorf("describe %s %s: registry response does not identify the requested resource", kind.singular(), resource)
	}

	if asJSON {
		return a.printResourceDetailJSON(&detail)
	}

	a.printResourceDetail(host, &detail)
	return nil
}

// printResourceDetailJSON writes the detail as one object with a stable key set. Nil slices are written as empty arrays.
func (a *app) printResourceDetailJSON(detail *resourceDetail) error {
	if detail.BlockReasons == nil {
		detail.BlockReasons = []string{}
	}

	if detail.Versions == nil {
		detail.Versions = []resourceVersion{}
	}

	for i := range detail.Versions {
		if detail.Versions[i].BlockReasons == nil {
			detail.Versions[i].BlockReasons = []string{}
		}
	}

	if detail.AgentMetadata != nil && detail.AgentMetadata.Tools == nil {
		detail.AgentMetadata.Tools = []string{}
	}

	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")

	return enc.Encode(detail)
}

// printResourceDetail writes the table form. Empty values are omitted. Author text is sanitized by writeDetailRow.
func (a *app) printResourceDetail(host string, detail *resourceDetail) {
	st := newStyler(a.out)
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, st.paint(ansiBold, "◆ "+detail.Namespace+"/"+detail.Name))
	writeDetailRow(a.out, st, "Kind", detail.Kind, "")
	writeDetailRow(a.out, st, "Registry", host, "")
	writeDetailRow(a.out, st, "Latest", detail.LatestVersion, "")

	syncState, syncColor := "not synced", ansiDim
	if detail.Synced {
		syncState, syncColor = "synced", ansiGreen
	}

	writeDetailRow(a.out, st, "Sync", joinNonEmpty(" · ", syncState, detail.SyncStatus), syncColor)
	if detail.HasUnsyncedVersions {
		writeDetailRow(a.out, st, "Unsynced", "yes", ansiYellow)
	}

	if detail.Blocked {
		writeDetailRow(a.out, st, "Blocked", "yes", ansiRed)
	}

	for _, reason := range detail.BlockReasons {
		writeDetailRow(a.out, st, "Reason", reason, "")
	}

	if detail.Public {
		writeDetailRow(a.out, st, "Public", "yes", "")
	}

	writeDetailRow(a.out, st, "Risk", detail.JevRiskLevel, "")
	writeDetailRow(a.out, st, "Repo", detail.RepoURL, "")
	writeDetailRow(a.out, st, "Owner", detail.RepoOwner, "")
	writeDetailRow(a.out, st, "Ref", detail.SourceRef, "")
	writeDetailRow(a.out, st, "Path", detail.SourcePath, "")
	writeDetailRow(a.out, st, "Commit", detail.SourceCommit, "")
	writeDetailRow(a.out, st, "File", detail.DefinitionFileName, "")

	if md := detail.AgentMetadata; md != nil {
		writeDetailRow(a.out, st, "Name", md.Name, "")
		writeDetailRow(a.out, st, "Model", md.Model, "")
		writeDetailRow(a.out, st, "Tools", strings.Join(md.Tools, ", "), "")
		writeDetailRow(a.out, st, "Description", md.Description, "")
	}

	if detail.TotalDownloads > 0 {
		writeDetailRow(a.out, st, "Downloads", strconv.FormatInt(detail.TotalDownloads, 10), "")
	}

	writeDetailRow(a.out, st, "Downloaded", detail.LastDownloadedAt, "")
	writeDetailRow(a.out, st, "Scanned", detail.LastScanned, "")
	if detail.ScanCounts != nil {
		writeDetailRow(a.out, st, "Scans", scanCountsLine(detail.ScanCounts), "")
	}

	if len(detail.Versions) == 0 {
		writeDetailRow(a.out, st, "Versions", "none reported", ansiDim)
	}

	for _, v := range detail.Versions {
		label := v.Version
		if label == "" {
			label = "no version assigned"
			if v.Commit != "" {
				label += " · commit " + v.Commit
			}
		}

		state := "not synced"
		color := ansiDim
		switch {
		case v.Blocked:
			state, color = "blocked", ansiRed
		case v.BranchPhase == "Rejected":
			state, color = "rejected", ansiRed
		case v.Synced:
			state, color = "synced", ansiGreen
		}

		writeDetailRow(a.out, st, "Version", label+" · "+state, color)
		for _, reason := range v.BlockReasons {
			writeDetailRow(a.out, st, "Reason", reason, "")
		}

		if !v.Synced && v.SyncStatus != "" && len(v.BlockReasons) == 0 {
			writeDetailRow(a.out, st, "Status", v.SyncStatus, "")
		}

		writeDetailRow(a.out, st, "Object", v.Name, "")
		writeDetailRow(a.out, st, "Platform", joinNonEmpty("/", v.OS, v.Arch), "")
		writeDetailRow(a.out, st, "Branch", v.BranchPhase, "")
		writeDetailRow(a.out, st, "File", v.FileName, "")
		writeDetailRow(a.out, st, "Checksum", v.Checksum, "")
		if v.ArchiveSizeBytes > 0 {
			writeDetailRow(a.out, st, "Size", strconv.FormatInt(v.ArchiveSizeBytes, 10)+" bytes", "")
		}

		if v.DownloadCount > 0 {
			writeDetailRow(a.out, st, "Downloads", strconv.FormatInt(v.DownloadCount, 10), "")
		}

		writeDetailRow(a.out, st, "Downloaded", v.LastDownloadedAt, "")
		writeDetailRow(a.out, st, "Scanned", v.LastScanned, "")
		if v.ScanCounts != nil {
			writeDetailRow(a.out, st, "Scans", scanCountsLine(v.ScanCounts), "")
		}

		writeDetailRow(a.out, st, "Schema", joinNonEmpty(" · ", v.SchemaState, v.SchemaMessage, v.SchemaAttemptedAt), "")
	}

	as := detail.JevAssessment
	if as == nil {
		writeDetailRow(a.out, st, "Assessment", "none reported", ansiDim)
	} else {
		a.printDescribeAssessment(st, as)
	}

	printJevPolicyDetails(a.out, st, detail)
}

// printJevPolicyDetails writes the exemption governance, unblock state, and thresholds that apply to a skill or agent.
func printJevPolicyDetails(w io.Writer, st styler, detail *resourceDetail) {
	if g := detail.JevExemptionGovernance; g != nil {
		writeDetailRow(w, st, "Governed by", g.GoverningPolicy, "")
		writeDetailRow(w, st, "Priority", strconv.FormatInt(int64(g.GoverningPriority), 10), "")
		writeDetailRow(w, st, "Match", joinNonEmpty(" · ", g.MatchingPolicy, g.MatchingState), "")
		if g.MatchingExpires != nil {
			writeDetailRow(w, st, "Expires", g.MatchingExpires.UTC().Format(time.RFC3339), "")
		}

		if g.Superseded {
			writeDetailRow(w, st, "Superseded", "yes", ansiYellow)
		}
	}

	if u := detail.JevUnblock; u != nil {
		writeDetailRow(w, st, "Unblock", u.State, "")
		writeDetailRow(w, st, "Digest", u.Digest, "")
		writeDetailRow(w, st, "Unblock by", u.GoverningPolicy, "")
	}

	if t := detail.JevThresholds; t != nil {
		thresholds := []struct {
			name  string
			value *float64
		}{
			{"min safe", t.MinSafeProbability},
			{"max injection", t.MaxInjectionProbability},
			{"max exfiltration", t.MaxExfiltrationProbability},
			{"max destructive", t.MaxDestructiveProbability},
			{"max hidden", t.MaxHiddenInstructionsProbability},
			{"max scope", t.MaxScopeMismatchProbability},
			{"max remote", t.MaxRemoteExecutionProbability},
			{"max risk", t.MaxRiskScore},
			{"min confidence", t.MinConfidence},
		}
		for _, th := range thresholds {
			if th.value != nil {
				writeDetailRow(w, st, "Threshold", th.name+" "+strconv.FormatFloat(*th.value, 'f', 2, 64), "")
			}
		}
	}
}

// scanCountsLine formats scan counts in severity order.
func scanCountsLine(c *resourceScanCounts) string {
	return fmt.Sprintf("critical %d · high %d · medium %d · low %d · unknown %d · exempted %d",
		c.Critical, c.High, c.Medium, c.Low, c.Unknown, c.Exempted)
}

// joinNonEmpty joins the non-empty parts with sep.
func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}

	return strings.Join(kept, sep)
}

// writeDetailRow writes a labeled row after sanitizing the text. Empty text writes nothing.
func writeDetailRow(w io.Writer, st styler, label, text, color string) {
	text = sanitizeText(text)
	if text == "" {
		return
	}

	writeBlockedRows(w, st, label, text, color)
}

// printDescribeAssessment writes the Jev assessment block for a skill or agent.
func (a *app) printDescribeAssessment(st styler, as *opendepotv1alpha1.JevAssessment) {
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, st.paint(ansiBold, "  Latest Jev assessment"))
	fmt.Fprintln(a.out, st.paint(ansiDim, "  Advisory scores; assessment may belong to a rejected candidate."))
	a.printAssessmentMetrics(st, as)
	fmt.Fprintf(a.out, "  %-12s %s\n", "Review", reviewLine(st, as.NeedsReview))
	if as.Blocked {
		writeDetailRow(a.out, st, "Blocked", "yes", ansiRed)
	} else {
		writeDetailRow(a.out, st, "Blocked", "no", ansiGreen)
	}

	if as.Exempted {
		exemption := "yes"
		if as.ExemptedBy != "" {
			exemption += " · " + as.ExemptedBy
		}

		writeDetailRow(a.out, st, "Exempted", exemption, ansiYellow)
	}

	for _, reason := range as.BlockReasons {
		writeDetailRow(a.out, st, "Reason", reason, "")
	}

	if as.Error == "" {
		writeDetailRow(a.out, st, "Error", "none reported", ansiDim)
	} else {
		writeDetailRow(a.out, st, "Error", as.Error, ansiRed)
	}
}
