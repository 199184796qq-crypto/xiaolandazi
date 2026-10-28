package addressing

import (
	"math/rand"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Viewer is the stable identity used everywhere: the account ID owns the name,
// so dedupe and cooldown never depend on alias strings lining up by index.
type Viewer struct {
	UserID  string
	RawName string
}

// Pick rolls 1..min(max, len(viewers)) and returns that many viewers, newest
// first when pick=recent. The roll decides who is inspected, not how many are
// finally named.
func Pick(viewers []Viewer, rules *Rules, r *rand.Rand) []Viewer {
	if len(viewers) == 0 {
		return nil
	}
	max := 1
	pick := "recent"
	if rules != nil {
		if rules.Dice.Max > 0 {
			max = rules.Dice.Max
		}
		pick = strings.ToLower(strings.TrimSpace(rules.Dice.Pick))
	}
	n := min(max, len(viewers))
	if n <= 0 {
		return nil
	}
	k := 1
	if r != nil {
		k = 1 + r.Intn(n)
	}
	if pick == "random" {
		perm := r.Perm(len(viewers))
		out := make([]Viewer, 0, k)
		for i := 0; i < k && i < len(perm); i++ {
			out = append(out, viewers[perm[i]])
		}
		return out
	}
	// recent: viewers arrive oldest first, so the tail is the newest.
	return append([]Viewer(nil), viewers[len(viewers)-k:]...)
}

// Form is the program-bounded shape a spoken address may take.
type Form string

const (
	FormFull        Form = "full"
	FormShort       Form = "short"
	FormFullTitle   Form = "full+title"
	FormShortTitle  Form = "short+title"
	FormPlaceholder Form = "placeholder"
)

// Picked is one viewer that will be named.
type Picked struct {
	Viewer
	Decision Decision
	Form     Form
	Text     string
}

// Plan is the single frozen addressing result for one interaction.
type Plan struct {
	Mode       string // single | group | mixed
	Picked     []Picked
	Grey       []Viewer // names the model must judge before they can be named
	Dropped    int      // non-compliant viewers folded into the group address
	GroupLabel string
}

// Build runs the program half of the pipeline: roll the batch, check every
// rolled name, and keep only speakable ones. Grey names are returned for one
// batched model judgement; dropped names never get a solo address.
func Build(viewers []Viewer, rules *Rules, r *rand.Rand) Plan {
	if rules == nil {
		defaults := DefaultRules()
		rules = &defaults
	}
	plan := Plan{GroupLabel: groupLabel(*rules, r)}
	viewers = dedupeViewers(viewers)
	if len(viewers) == 0 {
		plan.Mode = "group"
		return plan
	}
	if len(viewers) == 1 {
		decision := rules.Check(viewers[0].RawName)
		switch decision.Status {
		case StatusSafe, StatusRewritten:
			plan.Mode = "single"
			plan.Picked = []Picked{{Viewer: viewers[0], Decision: decision, Form: formFor(decision.Text, *rules), Text: decision.Text}}
		case StatusGrey:
			plan.Mode = "single"
			plan.Grey = []Viewer{viewers[0]}
		default:
			plan.Mode = "group"
			plan.Dropped = 1
		}
		return plan
	}
	rolled := Pick(viewers, rules, r)
	for _, viewer := range rolled {
		decision := rules.Check(viewer.RawName)
		switch decision.Status {
		case StatusSafe, StatusRewritten:
			plan.Picked = append(plan.Picked, Picked{Viewer: viewer, Decision: decision, Form: formFor(decision.Text, *rules), Text: decision.Text})
		case StatusGrey:
			plan.Grey = append(plan.Grey, viewer)
		default:
			plan.Dropped++
		}
	}
	switch {
	case len(plan.Picked) == 0 && len(plan.Grey) == 0:
		plan.Mode = "group"
	case len(plan.Picked) == 1 && len(plan.Grey) == 0:
		plan.Mode = "single"
	default:
		plan.Mode = "mixed"
	}
	resolveDuplicateAddresses(&plan, rules)
	return plan
}

// resolveDuplicateAddresses extends the spoken digits when two auto-generated
// accounts would otherwise answer to the same address in one batch.
func resolveDuplicateAddresses(plan *Plan, rules *Rules) {
	counts := make(map[string]int, len(plan.Picked))
	for _, picked := range plan.Picked {
		counts[picked.Text]++
	}
	for i := range plan.Picked {
		picked := &plan.Picked[i]
		if counts[picked.Text] <= 1 {
			continue
		}
		if picked.Decision.Reason != "generic_account_digits" && picked.Decision.Reason != "digits_only" {
			continue
		}
		for n := 4; n <= 12; n++ {
			var candidate string
			var ok bool
			if picked.Decision.Reason == "digits_only" {
				candidate, ok = DigitsAddress(picked.RawName, n)
			} else {
				candidate, ok = rules.HardBlock.AccountAddress(picked.RawName, n)
			}
			if !ok {
				break
			}
			if counts[candidate] > 0 {
				continue
			}
			counts[picked.Text]--
			picked.Text = candidate
			picked.Decision.Short = rules.ShortForm(candidate)
			counts[candidate] = 1
			break
		}
	}
}

func dedupeViewers(viewers []Viewer) []Viewer {
	seen := map[string]struct{}{}
	out := make([]Viewer, 0, len(viewers))
	for _, viewer := range viewers {
		id := strings.TrimSpace(viewer.UserID)
		if id != "" {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
		}
		out = append(out, viewer)
	}
	return out
}

func formFor(text string, rules Rules) Form {
	return FormFull
}

func groupLabel(rules Rules, r *rand.Rand) string {
	if len(rules.GroupLabel) == 0 {
		return "几位朋友"
	}
	if r == nil {
		return rules.GroupLabel[0]
	}
	return rules.GroupLabel[r.Intn(len(rules.GroupLabel))]
}

// Shorten returns a program-bounded short form: keep a trailing title when
// present, otherwise the leading runes up to the configured limit.
func Shorten(name string, rules Rules) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	suffix := ""
	best := ""
	for _, title := range rules.Rewrite.KeepSuffixTitles {
		if strings.HasSuffix(name, title) && utf8.RuneCountInString(title) > utf8.RuneCountInString(best) {
			best = title
			suffix = title
		}
	}
	limit := rules.Rewrite.LongNameMax
	if suffix != "" {
		limit = 2
	}
	runes := []rune(name)
	if len(runes) <= limit {
		return name
	}
	core := string(runes[:min(limit, len(runes))])
	if suffix != "" && !strings.HasSuffix(core, suffix) {
		return core + suffix
	}
	return core
}

func hasHanRune(r rune) bool { return unicode.Is(unicode.Han, r) }
