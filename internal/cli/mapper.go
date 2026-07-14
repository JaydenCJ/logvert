// mapper.go implements --map and --drop-field: post-parse field surgery
// that renames extra fields or lifts them into the canonical envelope, so
// nonstandard producers normalize without regex configs.
package cli

import (
	"fmt"
	"strings"

	"github.com/JaydenCJ/logvert/internal/event"
	"github.com/JaydenCJ/logvert/internal/parse"
)

// mapRule is one --map from=to instruction.
type mapRule struct {
	from, to  string
	canonical bool // to is an envelope key
}

type mapper struct {
	rules []mapRule
	drops []string
}

// canonicalTargets are the envelope keys --map may lift into. "source" is
// deliberately absent: it records what parser ran and must stay truthful.
var canonicalTargets = map[string]bool{
	"ts": true, "level": true, "msg": true, "host": true, "app": true, "pid": true,
}

// newMapper validates the --map and --drop-field flag values.
func newMapper(maps, drops []string) (*mapper, error) {
	m := &mapper{drops: drops}
	for _, spec := range maps {
		from, to, ok := strings.Cut(spec, "=")
		if !ok || from == "" || to == "" {
			return nil, fmt.Errorf("--map wants from=to, got %q", spec)
		}
		if to == "source" {
			return nil, fmt.Errorf("--map cannot target \"source\"; it records which parser matched")
		}
		m.rules = append(m.rules, mapRule{from: from, to: to, canonical: canonicalTargets[to]})
	}
	return m, nil
}

// apply runs every rule against one event, in flag order.
func (m *mapper) apply(ev *event.Event, opt parse.Options) {
	for _, r := range m.rules {
		v, ok := ev.Fields.Get(r.from)
		if !ok {
			continue
		}
		if !r.canonical {
			ev.Fields.Rename(r.from, r.to)
			continue
		}
		if parse.LiftValue(ev, r.to, v, opt) {
			ev.Fields.Drop(r.from)
		}
	}
	for _, key := range m.drops {
		ev.Fields.Drop(key)
	}
}
