// Package diff computes message-set differences between two OCPP versions.
package diff

import "sort"

// MessageChange describes field-level changes for one message.
type MessageChange struct {
	AddedFields   []string
	RemovedFields []string
}

// Diff is the full comparison result.
type Diff struct {
	AddedMessages   []string
	RemovedMessages []string
	ChangedMessages map[string]MessageChange
}

// Compute compares old and new action->fields maps.
func Compute(oldSet, newSet map[string][]string) Diff {
	d := Diff{ChangedMessages: map[string]MessageChange{}}
	for action := range newSet {
		if _, ok := oldSet[action]; !ok {
			d.AddedMessages = append(d.AddedMessages, action)
		}
	}
	for action := range oldSet {
		if _, ok := newSet[action]; !ok {
			d.RemovedMessages = append(d.RemovedMessages, action)
		}
	}
	for action, newFields := range newSet {
		oldFields, ok := oldSet[action]
		if !ok {
			continue
		}
		ch := MessageChange{}
		oldFieldSet := toSet(oldFields)
		newFieldSet := toSet(newFields)
		for _, f := range newFields {
			if !oldFieldSet[f] {
				ch.AddedFields = append(ch.AddedFields, f)
			}
		}
		for _, f := range oldFields {
			if !newFieldSet[f] {
				ch.RemovedFields = append(ch.RemovedFields, f)
			}
		}
		if len(ch.AddedFields) > 0 || len(ch.RemovedFields) > 0 {
			sort.Strings(ch.AddedFields)
			sort.Strings(ch.RemovedFields)
			d.ChangedMessages[action] = ch
		}
	}
	sort.Strings(d.AddedMessages)
	sort.Strings(d.RemovedMessages)
	return d
}

// Markdown renders the diff as a CHANGELOG section.
func (d Diff) Markdown(fromVer, toVer string) string {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add("## OCPP " + fromVer + " → " + toVer + "\n\n")
	if len(d.AddedMessages) > 0 {
		add("### Added messages\n\n")
		for _, m := range d.AddedMessages {
			add("- " + m + "\n")
		}
		add("\n")
	}
	if len(d.RemovedMessages) > 0 {
		add("### Removed messages\n\n")
		for _, m := range d.RemovedMessages {
			add("- " + m + "\n")
		}
		add("\n")
	}
	if len(d.ChangedMessages) > 0 {
		add("### Changed messages\n\n")
		changed := make([]string, 0, len(d.ChangedMessages))
		for k := range d.ChangedMessages {
			changed = append(changed, k)
		}
		sort.Strings(changed)
		for _, m := range changed {
			c := d.ChangedMessages[m]
			add("- **" + m + "**")
			if len(c.AddedFields) > 0 {
				add(" +[" + join(c.AddedFields) + "]")
			}
			if len(c.RemovedFields) > 0 {
				add(" -[" + join(c.RemovedFields) + "]")
			}
			add("\n")
		}
	}
	return string(b)
}

func toSet(xs []string) map[string]bool {
	s := make(map[string]bool, len(xs))
	for _, x := range xs {
		s[x] = true
	}
	return s
}

func join(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
