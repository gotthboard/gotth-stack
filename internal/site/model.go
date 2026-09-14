package site

type principle struct {
	Key      string
	Number   string
	Eyebrow  string
	Title    string
	Body     string
	Evidence string
}

type pageView struct {
	Selected principle
}

var principles = [...]principle{
	{
		Key:      "control",
		Number:   "01",
		Eyebrow:  "Control",
		Title:    "The operator stays in the loop.",
		Body:     "Plans are inspectable. Approval binds exact inputs. Automation does not quietly turn intent into authority.",
		Evidence: "Preview -> exact approval -> durable record",
	},
	{
		Key:      "trust",
		Number:   "02",
		Eyebrow:  "Trust",
		Title:    "Secrets do not become configuration confetti.",
		Body:     "Manifests name secret slots, never secret values. Components receive narrow grants instead of a universal bag of credentials.",
		Evidence: "Named slots -> revision binding -> least privilege",
	},
	{
		Key:      "recovery",
		Number:   "03",
		Eyebrow:  "Recovery",
		Title:    "Unknown is a state, not a success color.",
		Body:     "If a mutation may have happened before a crash, GOTTH Stack refuses blind retry and reports that reconciliation is required.",
		Evidence: "Durable intent -> observed result -> honest rollback",
	},
}

// principleByKey selects one entry from the fixed public-site principle set.
// Complexity: time O(p), Omega(1), tight Theta(p) in the miss/last-item case;
// auxiliary space O(1), Omega(1), tight Theta(1); variables: p is the fixed
// principle count (currently 3); delegated costs: string comparison is bounded
// by the fixed key limit.
func principleByKey(key string) (principle, bool) {
	for _, candidate := range principles {
		if candidate.Key == key {
			return candidate, true
		}
	}
	return principle{}, false
}
