package status

import (
	"fmt"
	"html"
	"io"
	"strings"
)

// WriteMarkdown renders an already evaluated report without reevaluating evidence.
func WriteMarkdown(w io.Writer, rep Report) error {
	var b strings.Builder
	outcome := "not satisfied"
	if rep.Satisfied {
		outcome = "satisfied"
	}
	fmt.Fprintf(&b, "# NARU status\n\nMode: %s\n\nRequired evidence: **%s**\n\n", markdownCell(rep.Mode), outcome)
	if rep.Unverified.Present {
		b.WriteString("**NOT VERIFIED:** an unverified record exists or cannot be inspected. Pre-push remains blocked.\n\n")
	}
	b.WriteString("| Check | State | Evidence | Reason |\n| --- | --- | --- | --- |\n")
	for _, row := range rep.Checks {
		evidence := ""
		if row.Provenance != nil {
			evidence = string(row.Provenance.Kind)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", markdownCell(row.Check), markdownCell(string(row.State)), markdownCell(evidence), markdownCell(row.Reason))
	}
	if len(rep.Surfaces) > 0 {
		b.WriteString("\n| Surface | Baseline state |\n| --- | --- |\n")
		for _, row := range rep.Surfaces {
			fmt.Fprintf(&b, "| %s | %s |\n", markdownCell(row.Surface), markdownCell(row.State))
		}
	}
	if rep.Policy != nil {
		fmt.Fprintf(&b, "\nPolicy: **%s**; known=%d, new=%d, resolved=%d.\n", markdownCell(rep.Policy.State), rep.Policy.Known, rep.Policy.New, rep.Policy.Resolved)
		if rep.Policy.Known > 0 || rep.Policy.Resolved > 0 {
			b.WriteString("\nKnown findings remain warning evidence; shrinking the baseline requires explicit human approval.\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func markdownCell(value string) string {
	value = html.EscapeString(value)
	return strings.NewReplacer("\\", "&#92;", "|", "&#124;", "\r", " ", "\n", " ", "`", "&#96;", "*", "&#42;", "_", "&#95;", "[", "&#91;", "]", "&#93;").Replace(value)
}
