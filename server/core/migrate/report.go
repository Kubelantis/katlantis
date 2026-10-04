package migrate

import (
	"fmt"
	"strings"
)

// Counts summarises notes by action.
func Counts(notes []Note) map[Action]int {
	c := map[Action]int{}
	for _, n := range notes {
		c[n.Action]++
	}
	return c
}

// NeedsAttention reports whether any behaviour was removed or needs review.
func NeedsAttention(notes []Note) bool {
	c := Counts(notes)
	return c[Removed] > 0 || c[Review] > 0
}

func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

// Report renders the notes as Markdown.
func Report(notes []Note) string {
	var b strings.Builder
	c := Counts(notes)
	fmt.Fprintf(&b, "# Workflow migration report\n\n")
	fmt.Fprintf(&b, "| Converted | Dropped | Removed | Review |\n|---|---|---|---|\n| %d | %d | %d | %d |\n\n", c[Converted], c[Dropped], c[Removed], c[Review])
	sections := []struct {
		action Action
		title  string
		intro  string
	}{
		{Review, "Needs review", "Check these by hand: the result may not behave exactly as before."},
		{Removed, "Removed: provide another way", "These ran custom commands that native workflows do not support."},
		{Converted, "Converted to native inputs", ""},
		{Dropped, "Dropped without losing behaviour", ""},
	}
	for _, s := range sections {
		var rows []Note
		for _, n := range notes {
			if n.Action == s.action {
				rows = append(rows, n)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", s.title)
		if s.intro != "" {
			fmt.Fprintf(&b, "%s\n\n", s.intro)
		}
		if s.action == Removed {
			b.WriteString("| File | Where | Command | What it is | Replace with |\n|---|---|---|---|---|\n")
			for _, n := range rows {
				label := n.Label
				if label == "" {
					label = "-"
				}
				replace := n.Suggestion
				if replace == "" {
					replace = n.Detail
				}
				command := "-"
				if n.Command != "" {
					command = "`" + cell(n.Command) + "`"
				}
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", n.File, cell(n.Location), command, label, cell(replace))
			}
		} else {
			b.WriteString("| File | Where | Detail |\n|---|---|---|\n")
			for _, n := range rows {
				detail := n.Detail
				if n.Command != "" {
					detail += " (`" + cell(n.Command) + "`)"
				}
				fmt.Fprintf(&b, "| %s | %s | %s |\n", n.File, cell(n.Location), cell(detail))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
