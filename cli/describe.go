package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type resourceDetail struct {
	Kind          string                           `json:"kind"`
	Namespace     string                           `json:"namespace"`
	Name          string                           `json:"name"`
	Versions      []resourceVersion                `json:"versions"`
	JevAssessment *opendepotv1alpha1.JevAssessment `json:"jevAssessment"`
}

type resourceVersion struct {
	Version      string   `json:"version"`
	Commit       string   `json:"commit"`
	Synced       bool     `json:"synced"`
	SyncStatus   string   `json:"syncStatus"`
	BranchPhase  string   `json:"branchPhase"`
	Blocked      bool     `json:"blocked"`
	BlockReasons []string `json:"blockReasons"`
}

func (a *app) describeResource(ctx context.Context, kind entryKind, resource, registry string) error {
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

	a.printResourceDetail(host, &detail)
	return nil
}

func (a *app) printResourceDetail(host string, detail *resourceDetail) {
	st := newStyler(a.out)
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, st.paint(ansiBold, "◆ "+detail.Namespace+"/"+detail.Name))
	writeBlockedRows(a.out, st, "Kind", detail.Kind, "")
	writeBlockedRows(a.out, st, "Registry", host, "")

	if len(detail.Versions) == 0 {
		writeBlockedRows(a.out, st, "Versions", "none reported", ansiDim)
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

		writeBlockedRows(a.out, st, "Version", label+" · "+state, color)
		for _, reason := range v.BlockReasons {
			writeBlockedRows(a.out, st, "Reason", reason, "")
		}

		if !v.Synced && v.SyncStatus != "" && len(v.BlockReasons) == 0 {
			writeBlockedRows(a.out, st, "Status", v.SyncStatus, "")
		}
	}

	as := detail.JevAssessment
	if as == nil {
		writeBlockedRows(a.out, st, "Assessment", "none reported", ansiDim)
		return
	}

	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, st.paint(ansiBold, "  Latest Jev assessment"))
	fmt.Fprintln(a.out, st.paint(ansiDim, "  Advisory scores; assessment may belong to a rejected candidate."))
	a.printAssessmentMetrics(st, as)
	fmt.Fprintf(a.out, "  %-12s %s\n", "Review", reviewLine(st, as.NeedsReview))
	if as.Blocked {
		writeBlockedRows(a.out, st, "Blocked", "yes", ansiRed)
	} else {
		writeBlockedRows(a.out, st, "Blocked", "no", ansiGreen)
	}

	if as.Exempted {
		exemption := "yes"
		if as.ExemptedBy != "" {
			exemption += " · " + as.ExemptedBy
		}

		writeBlockedRows(a.out, st, "Exempted", exemption, ansiYellow)
	}

	for _, reason := range as.BlockReasons {
		writeBlockedRows(a.out, st, "Reason", reason, "")
	}

	if as.Error == "" {
		writeBlockedRows(a.out, st, "Error", "none reported", ansiDim)
	} else {
		writeBlockedRows(a.out, st, "Error", as.Error, ansiRed)
	}
}
