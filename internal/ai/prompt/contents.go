package prompt

import (
	"fmt"
	"strings"
)

// ContentsInput is the request for the contents task (J6.6).
type ContentsInput struct {
	Grounding Grounding      `json:"grounding"`
	Container string         `json:"container"` // the container's path, "Box 12" or "3rd drawer · desk · study"
	Existing  []ExistingLine `json:"existing"`  // lines already logged for the container
}

// ExistingLine is a contents line already logged.
type ExistingLine struct {
	Name     string `json:"name"`
	Quantity int64  `json:"quantity"`
}

// ContentsLine is one line of a contents list (contents_lines).
type ContentsLine struct {
	Name          string  `json:"name" desc:"Search name in the household's language."`
	Quantity      *int64  `json:"quantity" desc:"The count; null when they can't be counted."`
	Category      *string `json:"category" enum:"category"`
	WorthOwnEntry bool    `json:"worth_own_entry" desc:"True for branded or valuable things."`
}

// ContentsAnswer is the list of what is in a container.
type ContentsAnswer struct {
	Lines []ContentsLine `json:"lines"`
}

func contentsUser(cats catalog) func(ContentsInput) string {
	return func(in ContentsInput) string {
		var b strings.Builder
		b.WriteString(in.Grounding.render(cats, gCategories|gSpellings))
		b.WriteString("\n<photos>\nall: the open container\n</photos>\nThe container:\n")
		b.WriteString(dataBlock("container", clip(oneLine(in.Container), 200)))
		if len(in.Existing) > 0 {
			var d strings.Builder
			for _, l := range in.Existing {
				fmt.Fprintf(&d, "%s ×%d\n", clip(oneLine(l.Name), 120), max(l.Quantity, 1))
			}
			b.WriteString("\nAlready logged for this container; leave these out:\n")
			b.WriteString(dataBlock("already_logged", d.String()))
		}
		b.WriteString("\nList what is in the container.")
		return b.String()
	}
}

func checkContents(a *ContentsAnswer) error {
	if len(a.Lines) > 60 {
		return fmt.Errorf("lines: %d lines; group alike things into one line with a count", len(a.Lines))
	}
	seen := map[string]int{}
	for i, l := range a.Lines {
		p := fmt.Sprintf("lines[%d]", i)
		name := strings.TrimSpace(l.Name)
		if name == "" || len([]rune(name)) > 100 {
			return fmt.Errorf("%s.name: give a short name (1 to 100 characters)", p)
		}
		if l.Quantity != nil && (*l.Quantity < 1 || *l.Quantity > 10000) {
			return fmt.Errorf("%s.quantity: %d is not a count; use null when uncountable", p, *l.Quantity)
		}
		k := strings.ToLower(name)
		if j, dup := seen[k]; dup {
			return fmt.Errorf("%s: %q repeats lines[%d]; one line per kind of thing, with a count", p, name, j)
		}
		seen[k] = i
	}
	return nil
}

func checkContentsIn(in ContentsInput, a *ContentsAnswer) error {
	for i, l := range a.Lines {
		for _, e := range in.Existing {
			if strings.EqualFold(strings.TrimSpace(l.Name), strings.TrimSpace(e.Name)) {
				return fmt.Errorf("lines[%d]: %q is already logged; leave it out", i, l.Name)
			}
		}
		if in.Container != "" && strings.EqualFold(strings.TrimSpace(l.Name), strings.TrimSpace(in.Container)) {
			return fmt.Errorf("lines[%d]: %q is the container itself; leave it out", i, l.Name)
		}
	}
	return nil
}
