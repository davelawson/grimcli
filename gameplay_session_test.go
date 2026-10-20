package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/davelawson/grim/api"
)

func jsonText(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func gameView() *api.MatchView {
	return &api.MatchView{ID: "match-one", Name: "Game $(touch injected)", Status: "playing", Revision: 0, RulesVersion: "rules", CatalogueVersion: "cards", Participants: []string{"me", "other"}, Ring: []string{"me", "other"}, Wizards: map[string]api.WizardView{"me": {Resources: map[string]int{"wis": 2}, Integrity: intPtr(3), InPlay: []api.CardInstance{}, Discard: []api.CardInstance{}}, "other": {Resources: map[string]int{}, Integrity: intPtr(3)}}, You: &api.PrivateView{UserID: "me", Hand: []api.CardInstance{}}, Turn: &api.Turn{ActiveWizard: "me", Number: 10, Phase: "actions", Step: "actions"}}
}
func intPtr(v int) *int { return &v }
func gameResponse(t *testing.T, v *api.MatchView) string {
	return jsonText(t, api.MatchResponse{Match: v})
}
func gameCard(id, design string) api.CardInstance {
	return api.CardInstance{ID: id, DesignID: design, Owner: "me", Location: "hand"}
}
func testCatalogue() api.Catalogue {
	return api.Catalogue{RulesVersion: "rules", CatalogueVersion: "cards", Cards: []api.CardDesign{
		{ID: "resource", Name: "Wis $(touch injected) ; *", Categories: []string{"resource"}, Resources: map[string]int{"wis": 1}},
		{ID: "minion", Name: "Minion", Categories: []string{"minion"}, PlayRequirements: map[string]int{"fire": 2}, Expertise: map[string]int{"might": 1}},
		{ID: "element_fire", Name: "Fire", Categories: []string{"element"}, Element: "fire"},
		{ID: "arcane_focus", Name: "Arcane Focus"},
		{ID: "keen_insight", Name: "Keen Insight", Timing: "support", PlayRequirements: map[string]int{"air": 1}},
		{ID: "flowing_form", Name: "Flowing Form", Timing: "support"},
		{ID: "subjugate_rival", Name: "Subjugate Rival"},
		{ID: "investigate", Name: "Investigate", Challenges: []api.ChallengeDesign{{ID: "investigate", Requirements: map[string]int{"might": 2}}}},
		{ID: "rumour_full_moon", Name: "Full Moon", Challenges: []api.ChallengeDesign{{ID: "moonlit_hunt"}}},
		{ID: "spell_healing_spring", Name: "Healing Spring", Abilities: []api.AbilityDesign{{ID: "heal", Timing: "action", Cost: map[string]int{"wis": 3}}}},
		{ID: "research_spell", Name: "Research"}, {ID: "purifying_steam", Name: "Purifying Steam"},
		{ID: "counterspell", Name: "Counterspell", Timing: "spell_reaction"},
		{ID: "scalding_mist", Name: "Scalding Mist", Timing: "combat_reaction"},
		{ID: "moat", Name: "Moat", Timing: "defence_reaction"},
		{ID: "deep_freeze", Name: "Deep Freeze", Timing: "action"},
		{ID: "scrying_pool", Name: "Scrying Pool", Timing: "action"},
	}}
}

// Exercise the actual reusable composer independently of navigation. No network
// or mocked JSON entry is involved: each input is a numbered user selection.
func composeGame(t *testing.T, v *api.MatchView, command, input string) ([]string, int, string) {
	t.Helper()
	dir := t.TempDir()
	helper, err := filepath.Abs("gameplay-session.sh")
	if err != nil {
		t.Fatal(err)
	}
	harness := `#!/usr/bin/env bash
set -u
source "$1"
game_details=$(cat "$2")
game_catalogue_data=$(cat "$3")
game_args=("$4")
game_used='[]'
menu_prompt() { printf '%s' "$1"; IFS= read -r input || exit 78; [[ "$input" != $'\e' ]]; }
case "$4" in
 choose) game_compose_choice ;;
 defend) game_minions && game_support defend ;;
 *) game_compose_card "$4" ;;
esac
status=$?
jq -cn --argjson status "$status" --args '{status:$status,args:$ARGS.positional}' -- "${game_args[@]}" > "$5"
`
	for path, text := range map[string]string{"harness": harness, "view": gameResponse(t, v), "catalogue": jsonText(t, testCatalogue())} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", filepath.Join(dir, "harness"), helper, filepath.Join(dir, "view"), filepath.Join(dir, "catalogue"), command, filepath.Join(dir, "result"))
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("composer: %v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "result"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status int
		Args   []string
	}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("hostile card name or ID executed")
	}
	return result.Args, result.Status, string(out)
}

func TestGuidedEveryChoice(t *testing.T) {
	for _, tc := range []struct {
		kind, input      string
		minimum, maximum int
		options          []string
		want             []string
	}{
		{"maintenance", "d\nd\n", 0, 2, []string{"asset1", "asset2"}, nil},
		{"maintenance", "1\nd\n1\nd\n", 0, 2, []string{"asset1", "asset2"}, []string{"--keep", "asset1", "--payment", "p"}},
		{"kneel", "1\n", 1, 1, []string{"kneel", "decline"}, []string{"--kneel=true"}},
		{"kneel", "2\n", 1, 1, []string{"kneel", "decline"}, []string{"--kneel=false"}},
		{"effects", "2\n", 1, 1, []string{"effect", "skip:effect"}, []string{"--effectid", "effect"}},
		{"effects", "1\n1\nd\n1\nd\n8\nd\n", 1, 1, []string{"effect", "skip:effect"}, []string{"--effectid", "effect", "--minion", "m", "--support", "f", "--support-domain", "f=might"}},
		{"research", "2\n", 1, 1, []string{"scrying_pool", "minion"}, []string{"--designid", "minion"}},
		{"end_discard", "1\nd\n", 1, 3, []string{"h1", "h2", "h3"}, []string{"--discard", "h1"}},
		{"end_discard", "d\n", 0, 3, []string{"h1", "h2", "h3"}, nil},
		{"hand_discard", "1\n2\n", 1, 1, []string{"h1"}, []string{"--cardid", "h1", "--disposition", "destroy"}},
		{"asset_discard", "2\n", 1, 1, []string{"asset1", "asset2"}, []string{"--cardid", "asset2"}},
		{"attack_loss", "1\n", 1, 1, []string{"asset1"}, []string{"--cardid", "asset1"}},
		{"scry", "1\n2\n1\nd\n", 1, 1, []string{"secret1", "secret2", "secret3"}, []string{"--take", "secret1", "--order", "secret3", "--order", "secret2"}},
	} {
		t.Run(tc.kind+"/"+tc.input, func(t *testing.T) {
			v := gameView()
			v.You.Hand = []api.CardInstance{gameCard("h1", "resource"), gameCard("h2", "resource"), gameCard("h3", "resource"), gameCard("f", "arcane_focus"), gameCard("p", "resource")}
			w := v.Wizards["me"]
			w.InPlay = []api.CardInstance{gameCard("m", "minion"), gameCard("asset1", "minion"), gameCard("asset2", "minion")}
			v.Wizards["me"] = w
			v.Choice = &api.RequiredChoice{ID: "choice", Actor: "me", Type: tc.kind, Options: tc.options, Minimum: tc.minimum, Maximum: tc.maximum}
			if tc.kind == "maintenance" {
				v.You.Hand = []api.CardInstance{gameCard("p", "resource")}
			}
			if tc.kind == "effects" {
				w := v.Wizards["me"]
				w.InPlay = w.InPlay[:1]
				v.Wizards["me"] = w
				v.You.Hand = []api.CardInstance{gameCard("f", "arcane_focus")}
				v.Choice.Effects = []api.EffectOption{{ID: "effect", Kind: "moonlit_hunt", Source: "moon", Optional: true}}
			}
			if tc.kind == "hand_discard" {
				v.Exchange = &api.ExchangeView{Resolving: &api.PendingEffectView{Kind: "flash_of_insight"}}
			}
			if tc.kind == "scry" {
				for _, id := range tc.options {
					v.Choice.Cards = append(v.Choice.Cards, gameCard(id, "resource"))
				}
				v.Choice.Private = true
			}
			args, status, out := composeGame(t, v, "choose", tc.input)
			command := map[string]string{"end_discard": "end-discard", "hand_discard": "hand-discard", "asset_discard": "asset-loss", "attack_loss": "asset-loss", "research": "choose-research"}[tc.kind]
			if command == "" {
				command = tc.kind
			}
			if tc.kind == "effects" {
				command = "undertake-hunt"
				if strings.HasPrefix(tc.input, "2") {
					command = "skip-effect"
				}
			}
			want := append([]string{command, "--choiceid", "choice"}, tc.want...)
			if status != 0 || !reflect.DeepEqual(args, want) {
				t.Fatalf("status %d\nargs %q\nwant %q\n%s", status, args, want, out)
			}
			if tc.kind == "scry" && !strings.Contains(out, "Private scry") {
				t.Fatal(out)
			}
		})
	}
}

func TestGuidedCardComposition(t *testing.T) {
	for _, tc := range []struct {
		name, command, input string
		hand                 []api.CardInstance
		want                 []string
	}{
		{"requirements enabled by support", "play-card", "1\n1\nd\n1\nd\n", []api.CardInstance{gameCard("m", "minion"), gameCard("e", "element_fire"), gameCard("p", "resource")}, []string{"--cardid", "m", "--support", "e", "--payment", "p"}},
		{"attack", "play-card", "1\n1\nd\n1\nd\nd\n", []api.CardInstance{gameCard("a", "subjugate_rival"), gameCard("f", "arcane_focus")}, []string{"--cardid", "a", "--minion", "m", "--support", "f"}},
		{"research element", "play-card", "1\n2\n3\n", []api.CardInstance{gameCard("r", "research_spell")}, []string{"--cardid", "r", "--element", "air"}},
		{"research discover", "play-card", "1\n1\n1\nd\n", []api.CardInstance{gameCard("r", "research_spell"), gameCard("e", "element_fire")}, []string{"--cardid", "r", "--element-card", "e"}},
		{"challenge support domains", "challenge", "1\n1\n1\nd\n1\n2\n3\n4\nd\n8\n9\n1\n2\nd\n1\nd\n", []api.CardInstance{gameCard("i", "investigate"), gameCard("f", "arcane_focus"), gameCard("k", "keen_insight"), gameCard("flow", "flowing_form"), gameCard("e", "element_fire"), gameCard("p", "resource")}, []string{"--cardid", "i", "--challengeid", "investigate", "--minion", "m", "--support", "f", "--support", "k", "--support", "flow", "--support", "e", "--support-domain", "f=might", "--support-domain", "k=popularity", "--support-domain", "flow=fire", "--support-domain", "flow=earth", "--payment", "p"}},
		{"support assisted healing", "play-card", "1\n1\nd\n1\n1\nd\n", []api.CardInstance{gameCard("f", "arcane_focus"), gameCard("f2", "arcane_focus"), gameCard("p", "resource")}, []string{"--cardid", "f", "--support", "f2", "--support-domain", "f2=fire", "--payment", "p"}},
		{"activation", "activate", "1\n1\nd\n1\nd\n", []api.CardInstance{gameCard("p", "resource")}, []string{"--cardid", "spring", "--abilityid", "heal", "--payment", "p"}},
		{"additional discard", "play-card", "1\nd\nd\n1\n", []api.CardInstance{gameCard("steam", "purifying_steam"), gameCard("h", "resource")}, []string{"--cardid", "steam", "--discard", "h"}},
		{"defence", "defend", "1\nd\n1\nd\n", []api.CardInstance{gameCard("f", "arcane_focus")}, []string{"--minion", "m", "--support", "f"}},
		{"counterspell", "react", "1\nd\nd\n2\n", []api.CardInstance{gameCard("c", "counterspell")}, []string{"--cardid", "c", "--target", "s2"}},
		{"two combat targets", "react", "1\nd\nd\n1\n2\nd\n", []api.CardInstance{gameCard("c", "scalding_mist")}, []string{"--cardid", "c", "--target", "d1", "--target", "d2"}},
		{"empty targets", "react", "1\nd\nd\nd\n", []api.CardInstance{gameCard("c", "moat")}, []string{"--cardid", "c"}},
		{"neighbor target", "play-card", "1\nd\nd\n1\n", []api.CardInstance{gameCard("c", "deep_freeze")}, []string{"--cardid", "c", "--target", "d1"}},
		{"literal IDs", "play-card", "1\nd\nd\n", []api.CardInstance{gameCard("literal $(touch injected) ; \n*", "resource")}, []string{"--cardid", "literal $(touch injected) ; \n*"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := gameView()
			v.You.Hand = tc.hand
			w := v.Wizards["me"]
			w.InPlay = []api.CardInstance{gameCard("m", "minion"), gameCard("spring", "spell_healing_spring")}
			v.Wizards["me"] = w
			w = v.Wizards["other"]
			w.InPlay = []api.CardInstance{gameCard("d1", "minion"), gameCard("d2", "minion")}
			v.Wizards["other"] = w
			v.Exchange = &api.ExchangeView{Priority: "me", Stack: []api.PendingEffectView{{ID: "attack", Kind: "subjugate_rival", Actor: "me", Target: "other", Minions: []string{"m"}, Defenders: []string{"d1", "d2"}}, {ID: "s1", Kind: "spell", Spell: true}, {ID: "s2", Kind: "spell", Spell: true}}}
			if tc.name == "empty targets" {
				v.Exchange.Stack[0].Actor = "other"
				v.Exchange.Stack[0].Target = "me"
			}
			args, status, out := composeGame(t, v, tc.command, tc.input)
			command := tc.command
			switch tc.name {
			case "attack":
				command = "attack"
			case "research element":
				command = "research-element"
			case "research discover":
				command = "research-spell"
			case "support assisted healing":
				command = "heal-with-focus"
			}
			want := append([]string{command}, tc.want...)
			if status != 0 || !reflect.DeepEqual(args, want) {
				t.Fatalf("status=%d\ngot %q\nwant %q\n%s", status, args, want, out)
			}
			if tc.name == "requirements enabled by support" && !strings.Contains(out, `Requires: {"fire":2}`) {
				t.Fatal("requirements hidden", out)
			}
		})
	}
}

func TestGuidedCancellationAndEmptySelection(t *testing.T) {
	for _, input := range []string{"0\n", "\x1b\n", "1\n0\n", "1\nd\n0\n"} {
		v := gameView()
		v.You.Hand = []api.CardInstance{gameCard("h", "resource")}
		_, status, _ := composeGame(t, v, "play-card", input)
		if status != 1 {
			t.Fatalf("%q status=%d", input, status)
		}
	}
	v := gameView()
	v.Choice = &api.RequiredChoice{ID: "q", Actor: "me", Type: "maintenance", Minimum: 0, Maximum: 0}
	args, status, _ := composeGame(t, v, "choose", "d\nd\n")
	if status != 0 || !reflect.DeepEqual(args, []string{"maintenance", "--choiceid", "q"}) {
		t.Fatal(status, args)
	}
}

func gameplaySession(t *testing.T, input string, replies map[string][]sessionReply) (sessionFixture, *exec.Cmd, *bytes.Buffer) {
	t.Helper()
	f := newSessionFixture(t, sessionConfig)
	tools := filepath.Dir(f.script)
	if err := os.WriteFile(filepath.Join(tools, "uuidgen"), []byte(draftUUIDStub), 0700); err != nil {
		t.Fatal(err)
	}
	cmd, out := f.command(t, "\n\n"+input)
	cmd.Env = append(cmd.Env, "PATH="+tools+":"+os.Getenv("PATH"), "DRAFT_UUID_LOG="+filepath.Join(f.dir, "uuids.log"))
	if replies == nil {
		replies = map[string][]sessionReply{}
	}
	replies["catalogue"] = []sessionReply{{Body: jsonText(t, api.CatalogueResponse{Catalogue: func() *api.Catalogue { c := testCatalogue(); return &c }()})}}
	f.replies(t, cmd, replies)
	return f, cmd, out
}
func repeatReplies(body string, n int) []sessionReply {
	r := make([]sessionReply, n)
	for i := range r {
		r[i].Body = body
	}
	return r
}

func TestSessionGameplayOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, want, avoid string
		edit              func(*api.MatchView)
	}{
		{"empty hand active", "1) Gameplay actions", "", func(v *api.MatchView) {}},
		{"other turn", "Awaiting: other", "1) Gameplay actions", func(v *api.MatchView) { v.Turn.ActiveWizard = "other" }},
		{"own priority", "1) Respond to Priority", "1) Gameplay actions", func(v *api.MatchView) { v.Turn.ActiveWizard = "other"; v.Exchange = &api.ExchangeView{Priority: "me"} }},
		{"other priority", "Priority: other", "1) Respond to Priority", func(v *api.MatchView) { v.Exchange = &api.ExchangeView{Priority: "other"} }},
		{"own choice while eliminated", "1) Resolve required choice", "1) Gameplay actions", func(v *api.MatchView) {
			w := v.Wizards["me"]
			w.Eliminated = true
			v.Wizards["me"] = w
			v.Choice = &api.RequiredChoice{ID: "q", Actor: "me", Type: "attack_loss"}
		}},
		{"other private choice", "Awaiting: other", "1) Resolve required choice", func(v *api.MatchView) {
			v.Choice = &api.RequiredChoice{ID: "q", Actor: "other", Type: "scry", Private: true}
		}},
		{"eliminated", "You are eliminated", "1) Gameplay actions", func(v *api.MatchView) { w := v.Wizards["me"]; w.Eliminated = true; v.Wizards["me"] = w }},
		{"victory retaining exchange", "Winners: me", "1) Respond to Priority", func(v *api.MatchView) {
			v.Status = "finished"
			v.Winners = []string{"me"}
			v.Exchange = &api.ExchangeView{Priority: "me"}
			v.Choice = &api.RequiredChoice{Actor: "me"}
		}},
		{"draw", "Draw / no winners", "1) Gameplay actions", func(v *api.MatchView) { v.Status = "finished"; o := "draw"; v.Outcome = &o }},
		{"legacy identity", "fresh match view", "1) Gameplay actions", func(v *api.MatchView) { v.You.UserID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := gameView()
			tc.edit(v)
			f, cmd, out := gameplaySession(t, "4\n1\nq\n", map[string][]sessionReply{"match": {{Body: gameResponse(t, v)}}})
			runSession(t, cmd, out)
			assertText(t, out, tc.want)
			if tc.avoid != "" && strings.Contains(out.String(), tc.avoid) {
				t.Fatal(out)
			}
			for _, args := range f.calls(t) {
				if gameplayOperation(args[0]) != "" {
					t.Fatal("unexpected mutation", args)
				}
			}
		})
	}
}

func TestSessionExactRetryAndFetchFailure(t *testing.T) {
	v := gameView()
	before := gameResponse(t, v)
	v.Revision = 7
	compose := gameResponse(t, v)
	v.Revision = 8
	after := gameResponse(t, v)
	f, cmd, out := gameplaySession(t, "4\n1\n1\n4\ns\nr\n0\nr\nq\n", map[string][]sessionReply{
		"match": {{Body: before}, {Body: compose}, {Failed: true}, {Body: after}}, "end-turn": {{Failed: true}, {Body: after}},
	})
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "matches", "match", "catalogue", "match", "end-turn", "end-turn", "match", "match")
	if !reflect.DeepEqual(calls[5], calls[6]) || draftArg(t, calls[5], "revision") != "7" {
		t.Fatal("retry changed input", calls)
	}
	uuids, err := os.ReadFile(filepath.Join(f.dir, "uuids.log"))
	if err != nil || strings.Count(string(uuids), "\n") != 1 {
		t.Fatal("UUID regenerated", string(uuids), err)
	}
	assertText(t, out, "Review submission", "Accepted; Retry fetch", "Revision: 8")
}

func TestSessionPendingSurvivesNavigationAndRefresh(t *testing.T) {
	v := gameView()
	before := gameResponse(t, v)
	v.Revision = 20
	newer := gameResponse(t, v)
	f, cmd, out := gameplaySession(t, "4\n1\n1\n4\ns\n0\n0\n0\n4\n1\n1\nf\n1\nr\nq\n", map[string][]sessionReply{
		"match": {{Body: before}, {Body: before}, {Body: newer}, {Body: newer}, {Body: newer}}, "end-turn": {{Failed: true}, {Body: newer}},
	})
	runSession(t, cmd, out)
	calls := f.calls(t)
	var posts [][]string
	catalogues := 0
	for _, args := range calls {
		if args[0] == "end-turn" {
			posts = append(posts, args)
		}
		if args[0] == "catalogue" {
			catalogues++
		}
	}
	if len(posts) != 2 || !reflect.DeepEqual(posts[0], posts[1]) || draftArg(t, posts[1], "revision") != "0" || catalogues != 1 {
		t.Fatal("pending state replaced", calls)
	}
	assertText(t, out, "Pending submission retained", "Revision: 20")
}

func TestSessionDiscardAndRebuild(t *testing.T) {
	v := gameView()
	before := gameResponse(t, v)
	v.Revision = 4
	newer := gameResponse(t, v)
	f, cmd, out := gameplaySession(t, "4\n1\n1\n4\ns\n0\n1\nd\ndiscard\n4\ns\nq\n", map[string][]sessionReply{
		"match": {{Body: before}, {Body: before}, {Body: newer}, {Body: newer}}, "end-turn": {{Failed: true}, {Body: newer}},
	})
	runSession(t, cmd, out)
	var posts [][]string
	for _, args := range f.calls(t) {
		if args[0] == "end-turn" {
			posts = append(posts, args)
		}
	}
	if len(posts) != 2 || draftArg(t, posts[0], "revision") != "0" || draftArg(t, posts[1], "revision") != "4" || draftArg(t, posts[0], "requestid") == draftArg(t, posts[1], "requestid") {
		t.Fatal(posts, out)
	}
}

func TestSessionGameplayCancelledAtReview(t *testing.T) {
	for _, cancel := range []string{"0\n", "\x1b"} {
		t.Run(fmt.Sprintf("%q", cancel), func(t *testing.T) {
			v := gameView()
			body := gameResponse(t, v)
			f, cmd, out := gameplaySession(t, "4\n1\n1\n4\n"+cancel+"q\n", map[string][]sessionReply{"match": repeatReplies(body, 2)})
			runSession(t, cmd, out)
			assertCommands(t, f, "login", "matches", "match", "catalogue", "match")
		})
	}
}

func TestSessionFreshFetchCanRevokeAction(t *testing.T) {
	v := gameView()
	before := gameResponse(t, v)
	v.Turn.ActiveWizard = "other"
	after := gameResponse(t, v)
	f, cmd, out := gameplaySession(t, "4\n1\n1\nq\n", map[string][]sessionReply{"match": {{Body: before}, {Body: after}}})
	runSession(t, cmd, out)
	assertCommands(t, f, "login", "matches", "match", "catalogue", "match")
	assertText(t, out, "No gameplay input is currently available", "Awaiting: other")
}

func TestSessionAccountChangeClearsPendingAndCatalogue(t *testing.T) {
	v := gameView()
	before := gameResponse(t, v)
	v.Revision = 4
	after := gameResponse(t, v)
	f, cmd, out := gameplaySession(t, "4\n1\n1\n4\ns\n0\n0\n0\n5\n1\n\n\n0\n4\n1\n1\n4\ns\nq\n", map[string][]sessionReply{
		"match": {{Body: before}, {Body: before}, {Body: after}, {Body: after}, {Body: after}}, "end-turn": {{Failed: true}, {Body: after}},
	})
	// A successful second login must fetch the pinned catalogue again.
	var replies map[string][]sessionReply
	raw, err := os.ReadFile(filepath.Join(f.dir, "replies.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &replies); err != nil {
		t.Fatal(err)
	}
	replies["catalogue"] = append(replies["catalogue"], replies["catalogue"][0])
	f.replies(t, cmd, replies)
	runSession(t, cmd, out)
	var posts [][]string
	catalogues, logins := 0, 0
	for _, args := range f.calls(t) {
		switch args[0] {
		case "end-turn":
			posts = append(posts, args)
		case "catalogue":
			catalogues++
		case "login":
			logins++
		}
	}
	if len(posts) != 2 || catalogues != 2 || logins != 2 || draftArg(t, posts[1], "revision") != "4" || draftArg(t, posts[0], "requestid") == draftArg(t, posts[1], "requestid") {
		t.Fatalf("account cache not cleared: %q\n%s", f.calls(t), out)
	}
}

func TestSessionAcceptedFetchSurvivesNavigation(t *testing.T) {
	v := gameView()
	body := gameResponse(t, v)
	f, cmd, out := gameplaySession(t, "4\n1\n1\n4\ns\n0\n0\n0\n0\n4\n1\nq\n", map[string][]sessionReply{
		"match": {{Body: body}, {Body: body}, {Failed: true}, {Body: body}}, "end-turn": {{Body: body}},
	})
	runSession(t, cmd, out)
	posts := 0
	for _, args := range f.calls(t) {
		if args[0] == "end-turn" {
			posts++
		}
	}
	if posts != 1 || strings.Contains(out.String(), "Pending submission retained") {
		t.Fatal("accepted request resent", f.calls(t), out)
	}
	assertText(t, out, "authoritative fetch still needed")
	last := out.String()[strings.LastIndex(out.String(), "Match: Game"):]
	if strings.Contains(last, "Resume pending input") {
		t.Fatal("successful fetch did not clear accepted input", last)
	}
}

func TestSessionReplayedResponseRequiresFreshIdentity(t *testing.T) {
	v := gameView()
	body := gameResponse(t, v)
	legacy := gameView()
	legacy.You.UserID = ""
	f, cmd, out := gameplaySession(t, "4\n1\n1\n4\ns\nq\n", map[string][]sessionReply{
		"match": repeatReplies(body, 3), "end-turn": {{Body: gameResponse(t, legacy)}},
	})
	runSession(t, cmd, out)
	assertCommands(t, f, "login", "matches", "match", "catalogue", "match", "end-turn", "match")
	// A legacy receipt is never used as the session view; the fresh GET is.
	if strings.Contains(out.String(), "identity unavailable") {
		t.Fatal("used legacy receipt", out)
	}
}

func TestSessionPendingLiteralMatchID(t *testing.T) {
	v := gameView()
	v.ID = "match $(touch injected) ; * [x] ' \""
	body := gameResponse(t, v)
	list := jsonText(t, map[string]any{"matches": []map[string]string{{"id": v.ID, "status": "playing"}}})
	f, cmd, out := gameplaySession(t, "4\n1\n1\n4\ns\nr\nq\n", map[string][]sessionReply{
		"matches": {{Body: list}}, "match": repeatReplies(body, 3), "end-turn": {{Failed: true}, {Body: body}},
	})
	runSession(t, cmd, out)
	var posts [][]string
	for _, args := range f.calls(t) {
		if args[0] == "end-turn" {
			posts = append(posts, args)
		}
	}
	if len(posts) != 2 || !reflect.DeepEqual(posts[0], posts[1]) || draftArg(t, posts[0], "id") != v.ID {
		t.Fatal("ID changed", f.calls(t), out)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("match ID executed")
	}
}

func TestGuidedPrivateChoicesAndOrderedCancellation(t *testing.T) {
	v := gameView()
	v.Choice = &api.RequiredChoice{ID: "q\n", Actor: "me", Type: "scry", Options: []string{"secret1", "secret2"}, Cards: []api.CardInstance{gameCard("secret1", "resource"), gameCard("secret2", "minion")}, Minimum: 1, Maximum: 1, Private: true}
	args, status, out := composeGame(t, v, "choose", "1\n0\n")
	if status != 1 || strings.Contains(strings.Join(args, ","), "--order") {
		t.Fatal("ordered Back submitted an order", args, out)
	}
	args, status, out = composeGame(t, v, "choose", "1\n1\n")
	if status != 0 || !reflect.DeepEqual(args, []string{"scry", "--choiceid", "q\n", "--take", "secret1", "--order", "secret2"}) {
		t.Fatal(status, args, out)
	}
	v.You.Hand = []api.CardInstance{gameCard("resource\n", "resource")}
	v.Choice = nil
	args, status, out = composeGame(t, v, "play-card", "1\nd\nd\n")
	if status != 0 || !reflect.DeepEqual(args, []string{"play-card", "--cardid", "resource\n"}) {
		t.Fatal("trailing newline lost", status, args, out)
	}
}

func TestGuidedOrdinaryEffectResolution(t *testing.T) {
	v := gameView()
	v.Choice = &api.RequiredChoice{ID: "effect/choice", Actor: "me", Type: "effects", Options: []string{"effect/one"}, Effects: []api.EffectOption{{ID: "effect/one", Kind: "whispering_wind_trigger", Source: "wind"}}, Minimum: 1, Maximum: 1}
	args, status, out := composeGame(t, v, "choose", "1\n")
	want := []string{"resolve-effect", "--choiceid", "effect/choice", "--effectid", "effect/one"}
	if status != 0 || !reflect.DeepEqual(args, want) {
		t.Fatal(status, args, out)
	}
}

func TestSessionRetainsDedicatedAttackAfterRefresh(t *testing.T) {
	v := gameView()
	v.You.Hand = []api.CardInstance{gameCard("attack\n$(touch injected)", "subjugate_rival")}
	w := v.Wizards["me"]
	w.InPlay = []api.CardInstance{gameCard("minion/one", "minion")}
	v.Wizards["me"] = w
	before := gameResponse(t, v)
	v.Revision = 20
	v.You.Hand = nil
	v.Turn.ActiveWizard = "other"
	newer := gameResponse(t, v)
	f, cmd, out := gameplaySession(t, "4\n1\n1\n1\n1\n1\nd\nd\nd\ns\nf\n1\nr\nq\n", map[string][]sessionReply{
		"match": {{Body: before}, {Body: before}, {Body: newer}, {Body: newer}}, "attack": {{Failed: true}, {Body: newer}},
	})
	runSession(t, cmd, out)
	var posts [][]string
	for _, args := range f.calls(t) {
		if args[0] == "play-card" {
			t.Fatal("stored generic card command", args)
		}
		if args[0] == "attack" {
			posts = append(posts, args)
		}
	}
	if len(posts) != 2 || !reflect.DeepEqual(posts[0], posts[1]) || draftArg(t, posts[1], "revision") != "0" || draftArg(t, posts[1], "cardid") != "attack\n$(touch injected)" {
		t.Fatal("dedicated route changed on refresh", f.calls(t), out)
	}
	assertText(t, out, "Review submission", "Revision: 20")
}
