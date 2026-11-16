package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	catalogPath         = "/opendepot/agents/v1/catalog"
	catalogPageSize     = 100
	maxCatalogPages     = 1000
	descriptionMaxRunes = 80
)

// terminalEscapePattern matches CSI and OSC escape sequences, and a bare escape with the byte after it.
var terminalEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b.`)

// listOptions are the flags shared by the list subcommands.
type listOptions struct {
	registry  string
	namespace string
	json      bool
}

// catalogItem is one entry of the agents.v1 catalog response. RiskLevel and RiskScore are empty when
// the server does not know the assessment, which is not the same as safe.
type catalogItem struct {
	Kind                 string                `json:"kind"`
	Namespace            string                `json:"namespace"`
	Name                 string                `json:"name"`
	Status               string                `json:"status"`
	LatestVersion        string                `json:"latestVersion"`
	RiskLevel            string                `json:"riskLevel"`
	RiskScore            *float64              `json:"riskScore"`
	UnavailableCandidate *unavailableCandidate `json:"unavailableCandidate"`
	Description          string                `json:"description"`
}

// catalogPage is one page of the agents.v1 catalog response.
type catalogPage struct {
	Items      []catalogItem `json:"items"`
	TotalCount int           `json:"totalCount"`
}

// listCatalog prints the Skills or Agents published by the registry. The registry comes from --registry,
// then the project default, then the single host used by the project sources. Listing is read-only.
func (a *app) listCatalog(ctx context.Context, kind entryKind, opts listOptions) error {
	proj, err := loadOptionalProject(a.path(configFileName))
	if err != nil {
		return err
	}

	host, err := resolveListRegistry(opts.registry, proj)
	if err != nil {
		return err
	}

	items, err := a.fetchCatalog(ctx, host, kind, opts.namespace)
	if err != nil {
		return err
	}

	if opts.json {
		return a.printCatalogJSON(items)
	}

	if len(items) == 0 {
		scope := ""
		if opts.namespace != "" {
			scope = fmt.Sprintf(" in namespace %q", opts.namespace)
		}

		fmt.Fprintf(a.out, "no %s found on %s%s\n", kind.plural(), host, scope)
		return nil
	}

	a.printCatalog(items)
	return nil
}

// fetchCatalog reads every page of the catalog and returns the items of kind. The catalog is not filtered by
// kind on the server, so pages are followed in full before filtering. Any failed page fails the whole listing.
func (a *app) fetchCatalog(ctx context.Context, host string, kind entryKind, namespace string) ([]catalogItem, error) {
	var items []catalogItem
	seen := 0

	for page := 1; ; page++ {
		if page > maxCatalogPages {
			return nil, fmt.Errorf("list catalog on %s: more than %d pages; refusing to show a partial list", host, maxCatalogPages)
		}

		q := url.Values{}
		q.Set("page", strconv.Itoa(page))
		q.Set("page_size", strconv.Itoa(catalogPageSize))
		if namespace != "" {
			q.Set("namespace", namespace)
		}

		body, err := a.get(ctx, a.rootURL(host, catalogPath)+"?"+q.Encode(), host)
		if err != nil {
			return nil, catalogError(host, err)
		}

		var p catalogPage
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("list catalog on %s: decode page %d: %w", host, page, err)
		}

		if len(p.Items) == 0 {
			break
		}

		seen += len(p.Items)
		for _, item := range p.Items {
			if item.Kind == kind.singular() {
				items = append(items, item)
			}
		}

		if seen >= p.TotalCount {
			break
		}
	}

	return items, nil
}

// printCatalogJSON writes the items as {"items": [...]} with each full description. No items is an empty array.
func (a *app) printCatalogJSON(items []catalogItem) error {
	if items == nil {
		items = []catalogItem{}
	}

	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")

	return enc.Encode(struct {
		Items []catalogItem `json:"items"`
	}{Items: items})
}

// catalogError maps a failed catalog request to a message that tells the user what to do next.
func catalogError(host string, err error) error {
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return fmt.Errorf("list catalog on %s: %w", host, err)
	}

	switch {
	case statusErr.code == 401 || statusErr.code == 403:
		return fmt.Errorf("list catalog on %s: not authorized (HTTP %d); run 'opendepot login %s' and retry", host, statusErr.code, host)
	case statusErr.code == 404:
		return fmt.Errorf("registry %s does not support the agents.v1 catalog (HTTP 404); it may be an older OpenDepot server", host)
	case statusErr.code >= 500:
		return fmt.Errorf("registry %s failed to list the catalog (HTTP %d); no results were shown", host, statusErr.code)
	default:
		return fmt.Errorf("list catalog on %s: HTTP %d", host, statusErr.code)
	}
}

// printCatalog renders one row per item.
func (a *app) printCatalog(items []catalogItem) {
	st := newStyler(a.out)

	rows := make([][4]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, [4]string{
			item.Namespace + "/" + item.Name,
			latestCell(item),
			riskCell(item),
			catalogStatus(item),
		})
	}

	widths := [4]int{len("SOURCE"), len("VERSION"), len("RISK"), len("STATUS")}
	for _, r := range rows {
		for i := range widths {
			width := utf8.RuneCountInString(r[i])
			widths[i] = max(widths[i], width)
		}
	}

	for i, item := range items {
		widths[3] = max(widths[3], utf8.RuneCountInString(rows[i][3]+candidateNote(item, rows[i][3])))
	}

	header := [4]string{"SOURCE", "VERSION", "RISK", "STATUS"}
	fmt.Fprintf(a.out, "%s  %s  %s  %s  %s\n",
		padCell(st, ansiBold, header[0], widths[0]),
		padCell(st, ansiBold, header[1], widths[1]),
		padCell(st, ansiBold, header[2], widths[2]),
		padCell(st, ansiBold, header[3], widths[3]),
		st.paint(ansiBold, "DESCRIPTION"))

	for i, r := range rows {
		item := items[i]
		note := candidateNote(item, r[3])
		fmt.Fprintf(a.out, "%s  %s  %s  %s",
			padCell(st, "", r[0], widths[0]),
			padCell(st, "", r[1], widths[1]),
			padCell(st, riskColor(item), r[2], widths[2]),
			st.paint(statusColor(r[3]), r[3]))

		if note != "" {
			fmt.Fprint(a.out, st.paint(statusColor(item.UnavailableCandidate.Status), note))
		}

		fmt.Fprint(a.out, strings.Repeat(" ", max(widths[3]-utf8.RuneCountInString(r[3]+note), 0)))
		fmt.Fprintf(a.out, "  %s", tableDescription(item.Description))
		fmt.Fprintln(a.out)

	}
}

// candidateNote is the plain-text note for an unavailable candidate whose status differs from the row status, or "".
func candidateNote(item catalogItem, status string) string {
	c := item.UnavailableCandidate
	if c == nil || statusLabel(c.Status) == status {
		return ""
	}

	return " · candidate " + statusLabel(c.Status)
}

// tableDescription is the first line of an author-declared description, safe for one terminal cell. Escape
// sequences are removed, control or bidi format characters become spaces, runs of spaces collapse, and the result is
// at most descriptionMaxRunes runes including the trailing ellipsis.
func tableDescription(description string) string {
	text := strings.TrimSpace(description)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}

	text = sanitizeLine(text)
	if runes := []rune(text); len(runes) > descriptionMaxRunes {
		text = string(runes[:descriptionMaxRunes-1]) + "…"
	}

	return text
}

// sanitizeLine removes escape sequences from one line and replaces control, bidi format, and line or paragraph
// separator characters with spaces. Runs of spaces collapse to one.
func sanitizeLine(line string) string {
	line = terminalEscapePattern.ReplaceAllString(line, "")
	line = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return ' '
		}

		return r
	}, line)

	return strings.Join(strings.Fields(line), " ")
}

// sanitizeText applies sanitizeLine to each line of a multi-line text and keeps the line breaks.
func sanitizeText(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = sanitizeLine(line)
	}

	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

func catalogStatus(item catalogItem) string {
	if item.LatestVersion != "" {
		return "available"
	}

	return statusLabel(item.Status)
}

// latestCell is the latest installable version, or "none" when nothing is installable.
func latestCell(item catalogItem) string {
	if item.LatestVersion == "" {
		return "none"
	}

	return item.LatestVersion
}

// riskCell is the risk level and score, or "unknown" when the server omits the assessment.
func riskCell(item catalogItem) string {
	if item.RiskLevel == "" {
		return "unknown"
	}

	if item.RiskScore == nil {
		return item.RiskLevel
	}

	return fmt.Sprintf("%s (%.1f)", item.RiskLevel, *item.RiskScore)
}

// riskColor colors a known risk level and leaves unknown risk plain.
func riskColor(item catalogItem) string {
	if item.RiskLevel == "" {
		return ""
	}

	return riskLevelColor(item.RiskLevel)
}

// statusColor colors an item status. Blocked and rejected are red or yellow, and installable is green.
func statusColor(status string) string {
	switch status {
	case "available":
		return ansiGreen
	case "blocked":
		return ansiYellow
	case "rejected", "assessmentFailed", "assessment failed":
		return ansiRed
	default:
		return ansiDim
	}
}

// padCell paints text and pads it to width. Padding is measured on the plain text so color codes do not shift columns.
func padCell(st styler, code, text string, width int) string {
	return st.paint(code, text) + strings.Repeat(" ", max(width-utf8.RuneCountInString(text), 0))
}
