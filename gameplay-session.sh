#!/usr/bin/env bash
# Sourced by grimcli-session.sh. Gameplay state is scoped to the current account.
declare -A game_catalogues=() game_pending=() game_accepted=()
game_clear() {
    game_catalogues=(); game_pending=(); game_accepted=()
    unset game_details game_catalogue_data game_args game_used game_selected
}

game_jq='
def design($id): first($catalogue.cards[]? | select(.id == $id)) // {id:$id,name:$id};
def printed:
 ["Cost: \(.cost // {}|tojson)", "Requires: \(.playrequirements // {}|tojson)",
  (if (.expertise // {}|length)>0 then "Expertise: \(.expertise|tojson)" else empty end),
  (if (.affinity // {}|length)>0 then "Affinity: \(.affinity|tojson)" else empty end),
  (if (.income // {}|length)>0 then "Income: \(.income|tojson)" else empty end),
  (if (.resources // {}|length)>0 then "Resources: \(.resources|tojson)" else empty end),
  (if (.upkeep // {}|length)>0 then "Upkeep: \(.upkeep|tojson)" else empty end),
  (if (.timing // "")!="" then "Timing: \(.timing)" else empty end),
  (if (.text // "")!="" then .text else empty end),
  (.abilities[]? | "Ability \(.id): \(.text) · \(.timing) · Cost: \(.cost // {}|tojson)"),
  (.challenges[]? | "Challenge \(.id): Requires \(.requirements // {}|tojson) · Cost: \(.cost // {}|tojson)\(if .requiresfocus then " · Focus required" else "" end)")
 ] | join(" · ");
def cardoption:
 . as $c | design(.designid) as $d |
 {id:$c.id,label:($d.name + " [" + ($c.id|tojson) + "]"),
 details:(($d|printed) + (if $c.exhausted then " · Exhausted" else "" end) +
   (if $c.deployedturn then " · Deployed turn: \($c.deployedturn)" else "" end) +
   (if ($c.expertise // {}|length)>0 then " · Instance Expertise: \($c.expertise|tojson)" else "" end) +
   (if $c.owner then " · Owner: \($c.owner)" else "" end))};
def lane:
 .match as $m | $m.you.userid as $u |
 if $m.status != "playing" then "terminal"
 elif ($u // "") == "" then "identity"
 elif $m.choice != null then (if $m.choice.actor == $u then "choice" else "waiting" end)
 elif $m.wizards[$u].eliminated then "eliminated"
 elif $m.exchange != null then (if $m.exchange.priority == $u then "priority" else "waiting" end)
 elif $m.turn.activewizard == $u and $m.turn.phase == "actions" then "actions"
 else "waiting" end;
'

game_lane() { jq -r --argjson catalogue '{}' "$game_jq lane" <<<"$1"; }

game_render() {
    local details=$1 id=$2 catalogue=${game_catalogues[$2]:-'{"cards":[]}'}
    jq -r --argjson catalogue "$catalogue" "$game_jq"'
      .match as $m |
      "\nMatch: \($m.name) [\($m.id)]", "Status: \($m.status) · Revision: \($m.revision)",
      "You: \($m.you.userid // "identity unavailable; refresh required")",
      (if $m.status == "playing" then "Game started." else empty end),
      "Step: \($m.turn.step // "—")",
      (if $m.status == "finished" then
         "Outcome: \($m.outcome // "unavailable")",
         (if ($m.winners // [] | length) > 0 then "Winners: \($m.winners|join(", "))" else "Draw / no winners." end)
       else empty end),
      "Turn: \($m.turn.number // "—") · Active wizard: \($m.turn.activewizard // "—") · Phase: \($m.turn.phase // "—") / \($m.turn.step // "—")",
      ($m.wizards | to_entries[] | .key as $u | .value as $w |
       "Wizard \($u)\(if $u == $m.you.userid then " (you)" else "" end)\(if $w.eliminated then " — eliminated" else "" end)",
       "  Resources: \($w.resources|tojson) · Integrity: \($w.integrity // "—") · Progress: \($w.victoryprogress|tojson)",
       "  Affinities: \($w.affinities|tojson) · Immune: \($w.immune // false) · Hand: \($w.handcount // 0) · Draw: \($w.drawcount // 0)",
       "  Table:", ($w.inplay[]? | cardoption | "    \(.label) \(.details)"),
       "  Rumours:", ($w.rumours[]? | cardoption | "    \(.label) \(.details)"),
       "  Discard:", ($w.discard[]? | cardoption | "    \(.label)")),
      "Your hand:", ($m.you.hand[]? | cardoption | "  \(.label) \(.details)"),
      "Stack (declaration order; last resolves first):",
      ($m.exchange.stack[]? | "  \(.id): \(.kind) · Actor: \(.actor) · Target: \(.target // "—") · Targets: \(.targets // []|tojson) · Minions: \(.minions // []|tojson) · Defenders: \(.defenders // []|tojson)"),
      (if $m.exchange.resolving then "Resolving: \($m.exchange.resolving.kind)" else empty end),
      (if $m.status == "finished" then "Game finished."
       elif $m.choice then "Required input: \($m.choice.type) · Awaiting: \($m.choice.actor) · Select \($m.choice.minimum)–\($m.choice.maximum)"
       elif $m.exchange then "Priority: \($m.exchange.priority // "resolving")"
       elif lane == "identity" then "A fresh match view with your identity is required."
       elif lane == "eliminated" then "You are eliminated; inspection only."
       elif lane == "actions" then "Your action."
       else "Awaiting: \($m.turn.activewizard // "server")" end)' <<<"$details"
    if [[ -n ${game_pending[$id]:-} ]]; then
        if [[ ${game_accepted[$id]:-false} == true ]]; then
            printf 'Submission accepted; authoritative fetch still needed.\n'
        else
            printf 'Pending submission retained. Retry it exactly or explicitly discard it before rebuilding.\n'
        fi
    fi
}

# All JSON argv serialization is lossless, including whitespace and newlines.
game_save_pending() {
    game_pending[$1]=$(jq -cn --args '$ARGS.positional' -- "${game_args[@]}")
    game_accepted[$1]=false
}

game_fetch() {
    request 'Match fetch' match --id "$1" || return 1
    match_details "$response" || { printf 'Invalid match details.\n' >&2; return 1; }
    game_details=$response
    if [[ ${game_accepted[$1]:-false} == true ]]; then
        unset 'game_pending[$1]' 'game_accepted[$1]'
    fi
    if jq -e '.match.you.userid != null and .match.status != "setup"' <<<"$game_details" >/dev/null; then
        game_catalogue "$1" || true
    fi
    return 0
}

game_catalogue() {
    local id=$1
    if [[ -n ${game_catalogues[$id]:-} ]] && jq -e --argjson c "${game_catalogues[$id]}" '
      .match.rulesversion == $c.rulesversion and .match.catalogueversion == $c.catalogueversion' <<<"$game_details" >/dev/null; then
        game_catalogue_data=${game_catalogues[$id]}
        return
    fi
    request 'Catalogue fetch' catalogue --id "$id" || return 1
    game_catalogue_data=$(jq -ce '.catalogue | select((.cards|type)=="array")' <<<"$response") || return 1
    if ! jq -e --argjson c "$game_catalogue_data" '.match |
        .rulesversion == $c.rulesversion and .catalogueversion == $c.catalogueversion' <<<"$game_details" >/dev/null; then
        printf 'Catalogue version does not match this match. Refresh before playing.\n' >&2
        return 1
    fi
    game_catalogues[$id]=$game_catalogue_data
}

# A submission accepted by the server can only be followed by GETs. Failure or
# navigation never discards an uncertain POST. Refresh never changes its argv.
game_send_pending() {
    local id=$1
    local -a saved_args
    if [[ ${game_accepted[$id]:-false} != true ]]; then
        mapfile -d '' -t saved_args < <(jq -j '.[] | ., "\u0000"' <<<"${game_pending[$id]}")
        if ! response=$("$cli" "${saved_args[@]}" "${common_args[@]}" --token "$token"); then
            printf 'Gameplay request failed; exact submission retained.\n' >&2
            return 1
        fi
        game_accepted[$id]=true
        printf 'Submission accepted.\n'
    fi
    game_fetch "$id"
}

game_pending_menu() {
    local id=$1
    while [[ -n ${game_pending[$id]:-} ]]; do
        if [[ ${game_accepted[$id]:-false} == true ]]; then
            menu_prompt 'Accepted; Retry fetch (r), Refresh (f), or Back (0): ' || return 1
            case "$input" in
                r|R|f|F) game_fetch "$id" && return 0 ;;
                0) return 1 ;;
            esac
        else
            menu_prompt 'Pending input: exact Retry (r), Refresh (f), Discard/rebuild (d), or Back (0): ' || return 1
            case "$input" in
                r|R) game_send_pending "$id" && return 0 ;;
                f|F) game_fetch "$id" && return 0 ;;
                d|D)
                    menu_prompt 'Discard the retained input? An uncertain request may already have succeeded. Type discard (0 Back): ' || continue
                    if [[ "$input" == discard ]]; then
                        unset 'game_pending[$id]' 'game_accepted[$id]'
                        return 2
                    fi
                    ;;
                0) return 1 ;;
            esac
        fi
    done
}

# Choices are objects with literal IDs and printable labels. 'd' finishes a
# selection, while 0/Escape cancels. Ordered pickers append in the chosen order.
game_pick() {
    local title=$1 options=$2 minimum=$3 maximum=$4 ordered=${5:-false} selected='[]' value count
    game_selected='[]'
    while true; do
        printf '\n%s (select %s–%s)\n' "$title" "$minimum" "$maximum"
        jq -r --argjson selected "$selected" 'to_entries[] | .value as $v |
          "\(.key+1)) \(if $selected|index($v.id) then "[selected] " else "" end)\($v.label)",
          (if ($v.details // "") != "" then "   \($v.details)" else empty end)' <<<"$options"
        [[ "$ordered" == true ]] && printf 'Current order: %s\n' "$selected"
        printf 'd) Done (empty allowed when minimum is 0)\n0) Back / cancel\n'
        menu_prompt 'Choose a number or d: ' || return 1
        [[ "$input" == 0 ]] && return 1
        count=$(jq 'length' <<<"$selected")
        if [[ "$input" == d || "$input" == D ]]; then
            if ((count >= minimum && count <= maximum)); then game_selected=$selected; return 0; fi
            printf 'Selection needs %s–%s entries.\n' "$minimum" "$maximum" >&2
            continue
        fi
        value=$(jq -c --arg n "$input" 'to_entries[] | select((.key+1|tostring)==$n) | .value.id' <<<"$options")
        [[ -n "$value" ]] || { printf 'Choose one of the listed numbers.\n' >&2; continue; }
        if jq -e --argjson v "$value" 'index($v) != null' <<<"$selected" >/dev/null; then
            if [[ "$ordered" != true ]]; then selected=$(jq -c --argjson v "$value" 'map(select(. != $v))' <<<"$selected"); fi
        elif ((count < maximum)); then
            selected=$(jq -c --argjson v "$value" '. + [$v]' <<<"$selected")
            if ((minimum == 1 && maximum == 1)); then game_selected=$selected; return 0; fi
        else
            printf 'Maximum reached; deselect a card or finish.\n' >&2
        fi
    done
}

game_append() {
    local flag=$1 item
    while IFS= read -r -d '' item; do game_args+=("$flag" "$item"); done < <(jq -j '.[] | ., "\u0000"' <<<"$game_selected")
}

game_exclude_selected() { game_used=$(jq -cn --argjson a "$game_used" --argjson b "$game_selected" '$a+$b'); }

game_hand_options() {
    jq -c --argjson catalogue "$game_catalogue_data" --argjson used "$game_used" "$game_jq"'
      .match as $m | [$m.you.hand[]? | select(.id as $id | $used|index($id)|not) |
       select('"$1"') | cardoption]' <<<"$game_details"
}

game_minions() {
    local opts context=${1:-challenge}
    opts=$(jq -c --arg context "$context" --argjson catalogue "$game_catalogue_data" "$game_jq"'
      .match as $m | [$m.wizards[$m.you.userid].inplay[]? |
      select(.exhausted|not) | select($context=="defend" or (.deployedturn // 0) < $m.turn.number) |
      select((design(.designid).categories|index("minion")) != null) | cardoption]' <<<"$game_details")
    game_pick 'Commit Minions (check readiness and printed Expertise)' "$opts" 0 "$(jq length <<<"$opts")" || return 1
    game_append --minion
    game_exclude_selected
}

game_payments() {
    local opts
    opts=$(game_hand_options '(design(.designid).categories | index("resource")) != null')
    game_pick 'Payments (stored resources are also available)' "$opts" 0 "$(jq length <<<"$opts")" || return 1
    game_append --payment
    game_exclude_selected
}

game_support() {
    local context=$1 opts chosen id design domains domain count maximum title
    local -a support_ids
    title='Pair support cards (printed requirements apply after Elements and Focus)'
    if [[ "$context" == research ]]; then
        opts=$(game_hand_options '.designid == "arcane_focus"')
        [[ $(jq length <<<"$opts") == 0 ]] && return 0
        maximum=1
        title='Pair Arcane Focus (provides 1 Wis; discovery still needs Element cards)'
    elif [[ "$context" == defend ]]; then
        opts=$(game_hand_options '.designid == "arcane_focus"')
    elif [[ "$context" == challenge ]]; then
        opts=$(game_hand_options '(design(.designid).categories|index("element")) != null or .designid == "arcane_focus" or design(.designid).timing == "support"')
    else
        opts=$(game_hand_options '(design(.designid).categories|index("element")) != null or .designid == "arcane_focus"')
    fi
    maximum=${maximum:-$(jq length <<<"$opts")}
    game_pick "$title" "$opts" 0 "$maximum" || return 1
    chosen=$game_selected
    game_append --support
    game_exclude_selected
    mapfile -d '' -t support_ids < <(jq -j '.[] | ., "\u0000"' <<<"$chosen")
    for id in "${support_ids[@]}"; do
        design=$(jq -r --arg id "$id" '.match.you.hand[]|select(.id==$id)|.designid' <<<"$game_details")
        count=0
        case "$design" in
            arcane_focus) [[ "$context" != attack && "$context" != defend ]] && count=1 ;;
            keen_insight) count=1 ;;
            flowing_form) count=2 ;;
        esac
        if ((count)); then
            domains='["fire","earth","air","water","nobility","church","infernal","might","popularity"]'
            opts=$(jq -c 'map({id:.,label:.})' <<<"$domains")
            game_pick "Support domains for $design [$id]" "$opts" "$count" "$count" || return 1
            while IFS= read -r -d '' domain; do game_args+=(--support-domain "$id=$domain"); done < <(jq -j '.[]|.,"\u0000"' <<<"$game_selected")
        fi
    done
}

game_targets() {
    local design=$1 opts minimum=1 maximum=1
    case "$design" in
        counterspell)
            opts=$(jq -c '[.match.exchange.stack[]? | select(.spell) | {id:.id,label:(.kind+" · "+.actor+" ["+(.id|tojson)+"]")} ]' <<<"$game_details")
            ;;
        deep_freeze)
            opts=$(jq -c --argjson catalogue "$game_catalogue_data" "$game_jq"'
              .match as $m | $m.you.userid as $u | ($m.ring|index($u)) as $i |
              [$m.ring[(($i+1)%($m.ring|length))],$m.ring[(($i-1)%($m.ring|length))]] as $neighbours |
              [$m.wizards | to_entries[] | select(.key as $k | $neighbours|index($k)) | .value.inplay[]? |
               select(.exhausted|not) | select((design(.designid).categories|index("minion")) != null) | cardoption]' <<<"$game_details")
            ;;
        firewall|dislocate|deflect|wildfire|molten_armour|scalding_mist|freezing_fog|moat|quagmire)
            opts=$(jq -c --argjson catalogue "$game_catalogue_data" "$game_jq"'
              .match as $m | first($m.exchange.stack[]? | select(.kind=="subjugate_rival")) as $a |
              (if $a.actor==$m.you.userid then ($a.defenders // []) else ($a.minions // []) end) as $ids |
              [$m.wizards[].inplay[]? | select(.id as $id|$ids|index($id)) | cardoption]' <<<"$game_details")
            case "$design" in scalding_mist|freezing_fog) maximum=2 ;; moat|quagmire) minimum=0; maximum=2 ;; esac
            ;;
        *) return 0 ;;
    esac
    game_pick 'Targets (selection order matters; for Mist/Fog choose Might ≤1 first)' "$opts" "$minimum" "$maximum" true || return 1
    game_append --target
}

game_modes() {
    local design=$1 opts
    case "$design" in
        research_spell)
            game_pick 'Research mode' '[{"id":"discover","label":"Discover a Spell (pair up to two different Elements for its pool)"},{"id":"element","label":"Gain an Element (do not pair Elements)"}]' 1 1 || return 1
            if [[ "$game_selected" == '["element"]' ]]; then
                game_args[0]=research-element
                opts='["fire","earth","air","water"]'
                game_pick 'Element to gain' "$(jq -c 'map({id:.,label:.})' <<<"$opts")" 1 1 || return 1
                game_append --element
            else
                game_args[0]=research-spell
            fi
            ;;
        arcane_focus) game_args[0]=heal-with-focus ;;
        subjugate_rival) game_args[0]=attack ;;
    esac
}

game_compose_card() {
    local command=$1 opts card design context=normal
    case "$command" in
        play-card) opts=$(game_hand_options 'design(.designid).timing != "support" and ((design(.designid).timing // "")|endswith("reaction")|not) and (design(.designid).categories|index("element")|not) and .designid != "investigate"') ;;
        react) opts=$(game_hand_options '
          design(.designid).timing as $timing |
          ([$m.exchange.stack[]? | select(.kind=="subjugate_rival")][0]) as $attack |
          ($timing=="spell_reaction" and any($m.exchange.stack[]?; .spell)) or
          ($attack != null and (
            ($timing=="attack_reaction" and $attack.actor==$m.you.userid) or
            ($timing=="defence_reaction" and $attack.target==$m.you.userid) or
            ($timing=="combat_reaction" and ($attack.actor==$m.you.userid or $attack.target==$m.you.userid))))') ;;
        challenge)
            context=challenge
            opts=$(jq -c --argjson catalogue "$game_catalogue_data" "$game_jq"'
              .match as $m | [($m.you.hand[]? | select(.designid=="investigate")),
                ($m.wizards[$m.you.userid].rumours[]? | select(.designid!="rumour_full_moon"))] | map(cardoption)' <<<"$game_details")
            ;;
        activate)
            opts=$(jq -c --argjson catalogue "$game_catalogue_data" "$game_jq"'
              .match as $m | [$m.wizards[$m.you.userid].inplay[]? |
              select(any(design(.designid).abilities[]?; .timing=="action")) | cardoption]' <<<"$game_details")
            ;;
    esac
    game_pick 'Choose a card (printed requirements shown; paired support can enable it)' "$opts" 1 1 || return 1
    game_append --cardid
    game_exclude_selected
    IFS= read -r -d '' card < <(jq -j '.[0],"\u0000"' <<<"$game_selected")
    design=$(jq -r --arg id "$card" '.match as $m | [ $m.you.hand[]?, $m.wizards[$m.you.userid].inplay[]?, $m.wizards[$m.you.userid].rumours[]? ][] | select(.id==$id) | .designid' <<<"$game_details")
    if [[ "$command" == challenge || "$command" == activate ]]; then
        local field=challenges flag=--challengeid
        [[ "$command" == activate ]] && { field=abilities; flag=--abilityid; }
        opts=$(jq -c --arg id "$design" --arg field "$field" '[.cards[] | select(.id==$id) | .[$field][]? | select($field=="challenges" or .timing=="action") | {id:.id,label:.id,details:tojson}]' <<<"$game_catalogue_data")
        game_pick 'Printed Challenge / ability' "$opts" 1 1 || return 1
        game_append "$flag"
    fi
    game_modes "$design" || return 1
    if [[ "$context" == challenge || "$design" == subjugate_rival ]]; then
        [[ "$design" == subjugate_rival ]] && context=attack
        game_minions || return 1
    fi
    case "${game_args[0]}" in
        research-element) game_support research || return 1 ;;
        research-spell)
            opts=$(game_hand_options '(design(.designid).categories|index("element")) != null')
            game_pick 'Elements supporting Research (up to two different Elements)' "$opts" 0 2 || return 1
            game_append --element-card
            game_exclude_selected
            game_support research || return 1
            ;;
        *) game_support "$context" && game_payments || return 1 ;;
    esac
    game_targets "$design" || return 1
    if [[ "$design" == purifying_steam ]]; then
        opts=$(game_hand_options 'true')
        game_pick 'Additional discard for Purifying Steam' "$opts" 1 1 || return 1
        game_append --discard
    fi
}

game_compose_choice() {
    local kind opts selected effect choice_id
    kind=$(jq -r '.match.choice.type' <<<"$game_details")
    IFS= read -r -d '' choice_id < <(jq -j '.match.choice.id,"\u0000"' <<<"$game_details")
    game_args+=(--choiceid "$choice_id")
    case "$kind" in maintenance|kneel|effects|research|end_discard|hand_discard|asset_discard|attack_loss|scry) ;;
        *) printf 'Unknown choice type; refresh to inspect the updated state.\n' >&2; return 1 ;;
    esac
    opts=$(jq -c --argjson catalogue "$game_catalogue_data" "$game_jq"'
      .match as $m | [ $m.you.hand[]?, $m.choice.cards[]?, $m.wizards[].inplay[]?, $m.wizards[].rumours[]? ] as $cards |
      [$m.choice.options[]? | . as $id |
       (first($cards[] | select(.id==$id)) // null) as $card |
       (first($m.choice.effects[]? | select(.id==$id or ("skip:"+.id)==$id)) // null) as $effect |
       if $card then $card|cardoption
       elif $effect then {id:$id,label:((if $id|startswith("skip:") then "Skip " else "Resolve " end)+$effect.kind+" ["+($effect.source|tojson)+"]"),details:($effect|tojson)}
       else {id:$id,label:(design($id).name),details:(design($id)|printed)} end]' <<<"$game_details")
    case "$kind" in
        maintenance) printf 'Select Assets to keep and pay their combined upkeep. Unselected Assets are discarded.\n' ;;
        end_discard) printf 'Select cards to discard; all unselected cards are retained.\n' ;;
        scry) printf 'Private scry: choose the card to take, then explicitly order every remaining card, top first.\n' ;;
    esac
    game_pick "Required choice: $kind" "$opts" "$(jq '.match.choice.minimum' <<<"$game_details")" "$(jq '.match.choice.maximum' <<<"$game_details")" || return 1
    selected=$game_selected
    case "$kind" in
        maintenance) game_args[0]=maintenance; game_append --keep ;;
        kneel)
            game_args[0]=kneel
            if [[ "$selected" == '["kneel"]' ]]; then game_args+=(--kneel=true); else game_args+=(--kneel=false); fi
            ;;
        effects)
            local selected_id
            IFS= read -r -d '' selected_id < <(jq -j '.[0],"\u0000"' <<<"$selected")
            effect=$(jq -r --arg chosen "$selected_id" '.match.choice.effects[]? | select(.id==$chosen) | .kind' <<<"$game_details")
            if [[ "$selected_id" == skip:* ]]; then
                game_args[0]=skip-effect
                game_args+=(--effectid "${selected_id#skip:}")
            elif [[ "$effect" == moonlit_hunt ]]; then
                game_args[0]=undertake-hunt
                game_args+=(--effectid "$selected_id")
            else
                game_args[0]=resolve-effect
                game_args+=(--effectid "$selected_id")
            fi
            ;;
        research) game_args[0]=choose-research; game_append --designid ;;
        end_discard) game_args[0]=end-discard; game_append --discard ;;
        hand_discard) game_args[0]=hand-discard; game_append --cardid ;;
        asset_discard|attack_loss) game_args[0]=asset-loss; game_append --cardid ;;
        scry) game_args[0]=scry; game_append --take ;;
    esac
    game_exclude_selected
    case "$kind" in
        maintenance) game_payments || return 1 ;;
        effects)
            effect=$(jq -r --argjson chosen "$selected" '.match.choice.effects[]? | select(.id==$chosen[0]) | .kind' <<<"$game_details")
            if [[ "$effect" == moonlit_hunt ]]; then
                game_minions || return 1
                game_support challenge || return 1
                game_payments || return 1
            fi
            ;;
        hand_discard)
            effect=$(jq -r '.match.exchange.resolving.kind' <<<"$game_details")
            if [[ "$effect" == flash_of_insight ]]; then
                game_pick 'Discard or destroy the selected card' '[{"id":"discard","label":"Discard"},{"id":"destroy","label":"Destroy"}]' 1 1 || return 1
                game_append --disposition
            else
                game_args+=(--disposition discard)
            fi
            ;;
        scry)
            opts=$(jq -c --argjson chosen "$selected" '[.[]|select(.id as $id|$chosen|index($id)|not)]' <<<"$opts")
            local count
            count=$(jq length <<<"$opts")
            game_pick 'Remaining scry cards, top first' "$opts" "$count" "$count" true || return 1
            game_append --order
            ;;
    esac
}

game_action() {
    local id=$1 lane command uuid result
    if [[ -n ${game_pending[$id]:-} ]]; then
        game_pending_menu "$id"
        result=$?
        [[ "$result" == 2 ]] || return "$result"
    fi
    # Always fetch before composing. The captured revision stays fixed below.
    game_fetch "$id" || return 1
    lane=$(game_lane "$game_details")
    case "$lane" in actions|priority|choice) ;; *) printf 'No gameplay input is currently available for you. Refresh to inspect.\n'; return 0 ;; esac
    game_catalogue "$id" || return 1
    case "$lane" in
        choice) command=choose ;;
        actions)
            printf '\n1) Play card / Attack / Research\n2) Attempt Challenge\n3) Activate ability\n4) End turn\n0) Back\n'
            menu_prompt 'Choose an action: ' || return 1
            case "$input" in 1) command=play-card ;; 2) command=challenge ;; 3) command=activate ;; 4) command=end-turn ;; 0) return 1 ;; *) return 1 ;; esac
            ;;
        priority)
            printf '\n1) Play reaction\n'
            local can_defend
            can_defend=$(jq -r '.match as $m | any($m.exchange.stack[]?; .kind=="subjugate_rival" and .target==$m.you.userid)' <<<"$game_details")
            [[ "$can_defend" == true ]] && printf '2) Commit defence\n'
            printf '3) Pass Priority\n0) Back\n'
            menu_prompt 'Choose a response: ' || return 1
            case "$input" in 1) command=react ;; 2) [[ "$can_defend" == true ]] || return 1; command=defend ;; 3) command=pass ;; 0) return 1 ;; *) return 1 ;; esac
            ;;
    esac
    command -v uuidgen >/dev/null 2>&1 || { printf 'uuidgen is required for gameplay.\n' >&2; return 1; }
    uuid=$(uuidgen) || return 1
    [[ -n "$uuid" ]] || return 1
    game_args=("$command" --id "$id" --revision "$(jq -r '.match.revision' <<<"$game_details")" --requestid "$uuid")
    game_used='[]'
    case "$command" in
        play-card|react|challenge|activate) game_compose_card "$command" || return 1 ;;
        choose) game_compose_choice || return 1 ;;
        defend) game_minions defend && game_support defend || return 1 ;;
    esac
    printf '\nReview submission (server checks legality):\n'
    printf '  %q' "${game_args[@]}"
    printf '\n'
    menu_prompt 'Send (s), or Back (0/Escape cancels): ' || return 1
    [[ "$input" == s || "$input" == S ]] || return 1
    game_save_pending "$id"
    game_send_pending "$id" && return 0
    game_pending_menu "$id"
}
