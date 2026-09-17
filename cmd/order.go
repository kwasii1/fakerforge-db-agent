package cmd

import (
	"fmt"
	"sort"
	"strings"
)

// orderTables topologically sorts tables parents-first from parent sets.
// Refs outside the pushed set must already be pruned by the caller.
// Self-references are allowed (generated rows are parent-ordered within
// a table). Cross-table cycles abort with the cycle members named.
// Among ready tables the input order wins (deterministic output).
func orderTables(tables []string, parents map[string][]string) ([]string, error) {
	emitted := make(map[string]bool, len(tables))
	ordered := make([]string, 0, len(tables))

	ready := func(t string) bool {
		for _, p := range parents[t] {
			if p == t {
				continue
			}
			if !emitted[p] {
				return false
			}
		}
		return true
	}

	for len(ordered) < len(tables) {
		progress := false
		for _, t := range tables {
			if emitted[t] {
				continue
			}
			if ready(t) {
				emitted[t] = true
				ordered = append(ordered, t)
				progress = true
			}
		}
		if !progress {
			var stuck []string
			for _, t := range tables {
				if !emitted[t] {
					stuck = append(stuck, t)
				}
			}
			sort.Strings(stuck)
			return nil, fmt.Errorf("circular foreign keys between tables: %s", strings.Join(stuck, ", "))
		}
	}
	return ordered, nil
}
