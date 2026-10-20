package main

import (
	"flag"
	"fmt"
	"github.com/davelawson/grim/api"
	"strings"
)

type gameplayInput struct {
	ChoiceID    string
	CardID      string
	Targets     []string
	Support     []api.CardSupport
	Payment     []string
	Discard     []string
	Minions     []string
	ChallengeID string
	AbilityID   string
	Element     string
	Elements    []string
	Keep        []string
	Kneel       bool
	EffectID    string
	DesignID    string
	Cards       []string
	Disposition string
	Take        string
	Order       []string
}

func gameplayOperation(command string) string {
	return map[string]string{
		"play-card":        "actions/play-card",
		"attack":           "actions/attack",
		"challenge":        "actions/attempt-challenge",
		"activate":         "actions/activate-ability",
		"research-element": "actions/research-element",
		"research-spell":   "actions/research-spell",
		"heal-with-focus":  "actions/heal-with-focus",
		"end-turn":         "actions/end-turn",
		"maintenance":      "choices/maintenance",
		"kneel":            "choices/kneel",
		"resolve-effect":   "choices/resolve-effect",
		"skip-effect":      "choices/skip-effect",
		"undertake-hunt":   "choices/undertake-hunt",
		"choose-research":  "choices/research",
		"end-discard":      "choices/end-discard",
		"hand-discard":     "choices/hand-discard",
		"asset-loss":       "choices/asset-loss",
		"scry":             "choices/scry",
		"react":            "reactions/play-card",
		"defend":           "reactions/defend",
		"pass":             "reactions/pass",
	}[command]
}

// Each command registers only fields accepted by its public request type.
func gameplayFlags(f *flag.FlagSet, o *options) {
	c := &o.gameplay
	f.Int64Var(&o.revision, "revision", 0, "Required expected revision (zero or greater)")
	f.StringVar(&o.requestID, "requestid", "", "Required canonical UUID; reuse UUID and arguments for exact retries")
	if strings.HasPrefix(gameplayOperation(o.command), "choices/") {
		f.StringVar(&c.ChoiceID, "choiceid", "", "Required pending choice ID (bound in URL)")
	}
	switch o.command {
	case "play-card":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
		repeatFlag(f, "target", &c.Targets)
		f.Func("support", "Paired card instance ID (repeatable)", func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("support card ID must not be blank")
			}
			c.Support = append(c.Support, api.CardSupport{CardID: s})
			return nil
		})
		repeatFlag(f, "support-domain", &o.supportDomains)
		repeatFlag(f, "payment", &c.Payment)
		repeatFlag(f, "discard", &c.Discard)
	case "attack":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
		repeatFlag(f, "minion", &c.Minions)
		f.Func("support", "Paired card instance ID (repeatable)", func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("support card ID must not be blank")
			}
			c.Support = append(c.Support, api.CardSupport{CardID: s})
			return nil
		})
		repeatFlag(f, "support-domain", &o.supportDomains)
		repeatFlag(f, "payment", &c.Payment)
	case "challenge":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
		f.StringVar(&c.ChallengeID, "challengeid", "", "Required challengeid")
		repeatFlag(f, "minion", &c.Minions)
		f.Func("support", "Paired card instance ID (repeatable)", func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("support card ID must not be blank")
			}
			c.Support = append(c.Support, api.CardSupport{CardID: s})
			return nil
		})
		repeatFlag(f, "support-domain", &o.supportDomains)
		repeatFlag(f, "payment", &c.Payment)
	case "activate":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
		f.StringVar(&c.AbilityID, "abilityid", "", "Required abilityid")
		f.Func("support", "Paired card instance ID (repeatable)", func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("support card ID must not be blank")
			}
			c.Support = append(c.Support, api.CardSupport{CardID: s})
			return nil
		})
		repeatFlag(f, "support-domain", &o.supportDomains)
		repeatFlag(f, "payment", &c.Payment)
	case "research-element":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
		f.StringVar(&c.Element, "element", "", "Required element")
	case "research-spell":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
		repeatFlag(f, "element-card", &c.Elements)
	case "heal-with-focus":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
		f.Func("support", "Paired card instance ID (repeatable)", func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("support card ID must not be blank")
			}
			c.Support = append(c.Support, api.CardSupport{CardID: s})
			return nil
		})
		repeatFlag(f, "support-domain", &o.supportDomains)
		repeatFlag(f, "payment", &c.Payment)
	case "end-turn":
	case "maintenance":
		repeatFlag(f, "keep", &c.Keep)
		repeatFlag(f, "payment", &c.Payment)
	case "kneel":
		f.Func("kneel", "Required explicit --kneel=true or --kneel=false", func(s string) error {
			if s != "true" && s != "false" {
				return fmt.Errorf("--kneel must be true or false")
			}
			c.Kneel = s == "true"
			return nil
		})
	case "resolve-effect":
		f.StringVar(&c.EffectID, "effectid", "", "Required effectid")
	case "skip-effect":
		f.StringVar(&c.EffectID, "effectid", "", "Required effectid")
	case "undertake-hunt":
		f.StringVar(&c.EffectID, "effectid", "", "Required effectid")
		repeatFlag(f, "minion", &c.Minions)
		f.Func("support", "Paired card instance ID (repeatable)", func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("support card ID must not be blank")
			}
			c.Support = append(c.Support, api.CardSupport{CardID: s})
			return nil
		})
		repeatFlag(f, "support-domain", &o.supportDomains)
		repeatFlag(f, "payment", &c.Payment)
	case "choose-research":
		f.StringVar(&c.DesignID, "designid", "", "Required designid")
	case "end-discard":
		repeatFlag(f, "discard", &c.Cards)
	case "hand-discard":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
		f.StringVar(&c.Disposition, "disposition", "", "Required disposition")
	case "asset-loss":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
	case "scry":
		f.StringVar(&c.Take, "take", "", "Required take")
		repeatFlag(f, "order", &c.Order)
	case "react":
		f.StringVar(&c.CardID, "cardid", "", "Required cardid")
		repeatFlag(f, "target", &c.Targets)
		f.Func("support", "Paired card instance ID (repeatable)", func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("support card ID must not be blank")
			}
			c.Support = append(c.Support, api.CardSupport{CardID: s})
			return nil
		})
		repeatFlag(f, "support-domain", &o.supportDomains)
		repeatFlag(f, "payment", &c.Payment)
	case "defend":
		repeatFlag(f, "minion", &c.Minions)
		f.Func("support", "Paired card instance ID (repeatable)", func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("support card ID must not be blank")
			}
			c.Support = append(c.Support, api.CardSupport{CardID: s})
			return nil
		})
		repeatFlag(f, "support-domain", &o.supportDomains)
	case "pass":
	}
}
func repeatFlag(f *flag.FlagSet, name string, values *[]string) {
	f.Func(name, "Value (repeatable; order preserved)", func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("--%s must not be blank", name)
		}
		*values = append(*values, s)
		return nil
	})
}

func validateGameplayOptions(o *options, explicit map[string]bool) error {
	for _, name := range []string{"id", "revision", "requestid"} {
		if !explicit[name] {
			return fmt.Errorf("--%s is required", name)
		}
	}
	if o.revision < 0 {
		return fmt.Errorf("--revision must be nonnegative")
	}
	if !canonicalUUID.MatchString(o.requestID) {
		return fmt.Errorf("--requestid must be a canonical UUID")
	}
	c := &o.gameplay
	if strings.HasPrefix(gameplayOperation(o.command), "choices/") && strings.TrimSpace(c.ChoiceID) == "" {
		return fmt.Errorf("--choiceid is required")
	}
	switch o.command {
	case "play-card":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
	case "attack":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
		if len(c.Minions) == 0 {
			return fmt.Errorf("--minion is required")
		}
	case "challenge":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
		if strings.TrimSpace(c.ChallengeID) == "" {
			return fmt.Errorf("--challengeid is required")
		}
	case "activate":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
		if strings.TrimSpace(c.AbilityID) == "" {
			return fmt.Errorf("--abilityid is required")
		}
	case "research-element":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
		if strings.TrimSpace(c.Element) == "" {
			return fmt.Errorf("--element is required")
		}
	case "research-spell":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
	case "heal-with-focus":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
	case "end-turn":
	case "maintenance":
	case "kneel":
		if !explicit["kneel"] {
			return fmt.Errorf("--kneel=true or --kneel=false is required")
		}
	case "resolve-effect":
		if strings.TrimSpace(c.EffectID) == "" {
			return fmt.Errorf("--effectid is required")
		}
	case "skip-effect":
		if strings.TrimSpace(c.EffectID) == "" {
			return fmt.Errorf("--effectid is required")
		}
	case "undertake-hunt":
		if strings.TrimSpace(c.EffectID) == "" {
			return fmt.Errorf("--effectid is required")
		}
	case "choose-research":
		if strings.TrimSpace(c.DesignID) == "" {
			return fmt.Errorf("--designid is required")
		}
	case "end-discard":
	case "hand-discard":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
		if strings.TrimSpace(c.Disposition) == "" {
			return fmt.Errorf("--disposition is required")
		}
		if c.Disposition != "discard" && c.Disposition != "destroy" {
			return fmt.Errorf("--disposition must be discard or destroy")
		}
	case "asset-loss":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
	case "scry":
		if strings.TrimSpace(c.Take) == "" {
			return fmt.Errorf("--take is required")
		}
	case "react":
		if strings.TrimSpace(c.CardID) == "" {
			return fmt.Errorf("--cardid is required")
		}
	case "defend":
	case "pass":
	}
	support := map[string]int{}
	for i, s := range c.Support {
		if _, exists := support[s.CardID]; exists {
			return fmt.Errorf("duplicate --support card %q", s.CardID)
		}
		support[s.CardID] = i
	}
	for _, pair := range o.supportDomains {
		// Split at the last '=' so IDs containing '=' remain literal.
		pos := strings.LastIndex(pair, "=")
		if pos <= 0 || strings.TrimSpace(pair[pos+1:]) == "" {
			return fmt.Errorf("--support-domain requires CARDID=DOMAIN")
		}
		index, ok := support[pair[:pos]]
		if !ok {
			return fmt.Errorf("--support-domain requires a matching --support")
		}
		c.Support[index].Domains = append(c.Support[index].Domains, pair[pos+1:])
	}
	return nil
}
func gameplayPayload(o options) (string, any) {
	c := o.gameplay
	if c.Keep == nil {
		c.Keep = []string{}
	}
	if c.Cards == nil {
		c.Cards = []string{}
	}
	if c.Order == nil {
		c.Order = []string{}
	}
	switch o.command {
	case "play-card":
		return api.PlayCardPath(o.id), api.PlayCardRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID, Targets: c.Targets, Support: c.Support, Payment: c.Payment, Discard: c.Discard}
	case "attack":
		return api.AttackPath(o.id), api.AttackRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID, Minions: c.Minions, Support: c.Support, Payment: c.Payment}
	case "challenge":
		return api.AttemptChallengePath(o.id), api.AttemptChallengeRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID, ChallengeID: c.ChallengeID, Minions: c.Minions, Support: c.Support, Payment: c.Payment}
	case "activate":
		return api.ActivateAbilityPath(o.id), api.ActivateAbilityRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID, AbilityID: c.AbilityID, Support: c.Support, Payment: c.Payment}
	case "research-element":
		return api.ResearchElementPath(o.id), api.ResearchElementRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID, Element: c.Element}
	case "research-spell":
		return api.ResearchSpellPath(o.id), api.ResearchSpellRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID, Elements: c.Elements}
	case "heal-with-focus":
		return api.HealWithFocusPath(o.id), api.HealWithFocusRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID, Support: c.Support, Payment: c.Payment}
	case "end-turn":
		return api.EndTurnPath(o.id), api.EndTurnRequest{RequestID: o.requestID, ExpectedRevision: &o.revision}
	case "maintenance":
		return api.MaintenancePath(o.id, c.ChoiceID), api.MaintenanceRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, Keep: c.Keep, Payment: c.Payment}
	case "kneel":
		return api.KneelPath(o.id, c.ChoiceID), api.KneelRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, Kneel: &c.Kneel}
	case "resolve-effect":
		return api.ResolveEffectPath(o.id, c.ChoiceID), api.ResolveEffectRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, EffectID: c.EffectID}
	case "skip-effect":
		return api.SkipEffectPath(o.id, c.ChoiceID), api.SkipEffectRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, EffectID: c.EffectID}
	case "undertake-hunt":
		return api.UndertakeHuntPath(o.id, c.ChoiceID), api.UndertakeHuntRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, EffectID: c.EffectID, Minions: c.Minions, Support: c.Support, Payment: c.Payment}
	case "choose-research":
		return api.ChooseResearchPath(o.id, c.ChoiceID), api.ChooseResearchRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, DesignID: c.DesignID}
	case "end-discard":
		return api.EndDiscardPath(o.id, c.ChoiceID), api.EndDiscardRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, Cards: c.Cards}
	case "hand-discard":
		return api.HandDiscardPath(o.id, c.ChoiceID), api.HandDiscardRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID, Disposition: c.Disposition}
	case "asset-loss":
		return api.AssetLossPath(o.id, c.ChoiceID), api.AssetLossRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID}
	case "scry":
		return api.ScryPath(o.id, c.ChoiceID), api.ScryRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, Take: c.Take, Order: c.Order}
	case "react":
		return api.ReactPath(o.id), api.ReactRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, CardID: c.CardID, Targets: c.Targets, Support: c.Support, Payment: c.Payment}
	case "defend":
		return api.DefendPath(o.id), api.DefendRequest{RequestID: o.requestID, ExpectedRevision: &o.revision, Minions: c.Minions, Support: c.Support}
	case "pass":
		return api.PassPath(o.id), api.PassRequest{RequestID: o.requestID, ExpectedRevision: &o.revision}
	}
	panic("unknown gameplay command")
}
