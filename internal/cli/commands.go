package cli

import (
	"slices"
	"sort"
	"strings"

	"github.com/judoscale/judoscale-cli/internal/spec"
)

// A group is a resource, from an operation tag: `judo <group> <command>`.
type group struct {
	name        string
	description string
	commands    []command
}

type command struct {
	group, name string
	op          spec.Operation
}

func (c command) fullName() string { return c.group + " " + c.name }

// commandTree groups the spec's operations into resources, named from their
// tags, in the spec's tag order (undeclared tags follow alphabetically, then
// untagged operations under "other").
//
// The group drops a leading parent word taken from the path ("Process
// Schedule" on /processes/{process_id}/schedule is "schedule"), and the
// command is the operationId without the tag and parent words
// (getProcessSchedule is "get"). Clashes fall back to longer names.
func commandTree(s *spec.Spec) []group {
	type entry struct {
		op          spec.Operation
		parent      string
		short, full string // group names
		tagWords    []string
		group, name string
	}
	var entries []*entry
	shorten := map[string]bool{} // tags whose leading word is a parent
	for _, op := range s.Operations {
		e := &entry{op: op, parent: parentWord(op), short: "other", full: "other"}
		if op.Tag != "" {
			e.tagWords = strings.Fields(strings.ToLower(op.Tag))
			e.full = strings.Join(e.tagWords, "-")
			e.short = e.full
			if len(e.tagWords) > 1 && singular(e.tagWords[0]) == e.parent {
				shorten[op.Tag] = true
			}
		}
		entries = append(entries, e)
	}
	shortTags := map[string]map[string]bool{}
	for _, e := range entries {
		if shorten[e.op.Tag] {
			e.short = strings.Join(e.tagWords[1:], "-")
		}
		if shortTags[e.short] == nil {
			shortTags[e.short] = map[string]bool{}
		}
		shortTags[e.short][e.op.Tag] = true
	}

	byGroup := map[string][]*entry{}
	for _, e := range entries {
		e.group = e.short
		if len(shortTags[e.short]) > 1 {
			e.group = e.full
		}
		byGroup[e.group] = append(byGroup[e.group], e)
	}

	descriptions := map[string]string{}
	var order []string
	for _, t := range s.Tags {
		for g, es := range byGroup {
			if es[0].op.Tag == t.Name && !slices.Contains(order, g) {
				order = append(order, g)
				descriptions[g] = t.Description
			}
		}
	}
	var rest []string
	for g := range byGroup {
		if g != "other" && !slices.Contains(order, g) {
			rest = append(rest, g)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)
	if _, ok := byGroup["other"]; ok {
		order = append(order, "other")
	}

	groups := make([]group, 0, len(order))
	for _, g := range order {
		es := byGroup[g]
		names := func(keepParent bool, only map[string]bool) map[string]int {
			count := map[string]int{}
			for _, e := range es {
				if only == nil || only[e.name] {
					e.name = commandWords(e.op.ID, e.tagWords, e.parent, keepParent)
				}
				count[e.name]++
			}
			return count
		}
		count := names(false, nil)
		if clash := clashes(count); len(clash) > 0 {
			count = names(true, clash)
			for _, e := range es {
				if count[e.name] > 1 {
					e.name = kebab(e.op.ID)
				}
			}
		}
		var cmds []command
		for _, e := range es {
			cmds = append(cmds, command{group: g, name: e.name, op: e.op})
		}
		sort.Slice(cmds, func(i, j int) bool { return cmds[i].name < cmds[j].name })
		groups = append(groups, group{name: g, description: descriptions[g], commands: cmds})
	}
	return groups
}

func clashes(count map[string]int) map[string]bool {
	clash := map[string]bool{}
	for name, n := range count {
		if n > 1 {
			clash[name] = true
		}
	}
	return clash
}

// parentWord is the resource of the first {x_id} path param, e.g. "process".
func parentWord(op spec.Operation) string {
	for _, p := range op.PathParams {
		if parent, ok := strings.CutSuffix(p.Name, "_id"); ok {
			return parent
		}
	}
	return ""
}

// commandWords drops the tag's words, and the parent word unless keepParent,
// from an operationId: listAppProcesses in "Processes" is "list".
func commandWords(id string, tagWords []string, parent string, keepParent bool) string {
	drop := map[string]bool{}
	for _, w := range tagWords {
		drop[singular(w)] = true
	}
	if keepParent {
		delete(drop, parent)
	} else if parent != "" {
		drop[parent] = true
	}
	var kept []string
	for _, w := range strings.Split(kebab(id), "-") {
		if !drop[singular(w)] {
			kept = append(kept, w)
		}
	}
	if len(kept) == 0 {
		return kebab(id)
	}
	return strings.Join(kept, "-")
}

// singular handles the plurals in tag names: apps, processes, metrics.
func singular(w string) string {
	switch {
	case strings.HasSuffix(w, "sses"):
		return strings.TrimSuffix(w, "es")
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		return strings.TrimSuffix(w, "s")
	}
	return w
}

func findGroup(groups []group, name string) *group {
	for i := range groups {
		if groups[i].name == name {
			return &groups[i]
		}
	}
	return nil
}

func (g *group) find(name string) *command {
	for i := range g.commands {
		if g.commands[i].name == name {
			return &g.commands[i]
		}
	}
	return nil
}
