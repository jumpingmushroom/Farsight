# Activity timeline

Status: approved design, 2026-10-05. Implements design section 05
(`design/World Atlas.dc.html` `:837-908`), which the MVP left out
(DESIGN-NOTES §1.8, §3.8).

## Goal

"Full timeline →" (desktop side panel, mobile main menu) opens an Activity
view: who was on today, filter chips by kind and by player, and a day-grouped
list of events from the server log and from world saves, newest first,
starting with the last 3 days and loading earlier days on demand back to when
tracking began (30 Sep 2026).

## Events

| Category (chip) | Event | Source | Text |
|---|---|---|---|
| Joins & leaves | player joined / left | server log (stored today) | "Ragnar joined", "Bjørn left after 3h 05m" |
| Server | starting, up, stopped, autosave, new join code | server log (stored today) | "Server starting", "Server is up", "Autosave finished · map updated", "New join code 252 289" |
| Deaths | new tombstone | world save diff | "New tombstone: Ragnar, near a sunken crypt in the Swamp" |
| Portals | new portal; later paired | world save diff | "New portal "copper", not paired with anything yet" / "… paired with "copper"" |
| Tames | new tame | world save diff | "New tame: Big Mama (Lox)" |
| Building | new base; base grew ≥ 25 pieces | world save diff | "New base: Lox Ranch (Plains)", "Lox Ranch grew by 120 pieces" |
| Bosses | boss key newly set | world save diff | "Moder defeated" |
| Raids & events | "Random event set: …" | server log (agent update) | "Raid: The forest is moving" (event name mapped to the game's message; unmapped names shown as-is) |

- Server-log events carry their exact time and the source line "Server log".
- World-save events carry the save's time ("World save · 14:20"), never the
  exact moment, plus "Show on map →" when the place is in explored ground.
- A world event whose place lies in unexplored ground under the *current*
  explored map is hidden from the activity API entirely, including the
  category counts, until that ground is explored.
- Place wording: the nearest known location within 300 m ("near a sunken
  crypt") and the biome, but only when both the event's point and that
  location are explored; otherwise the biome alone.
- Each event records the players it concerns (joiner, tombstone owner, portal
  creator, tame namer, base builders) for the people filter.

## Deriving world-save events

- The central app compares each newly stored snapshot with the previous one
  for that server and writes the differences to the existing `events` table
  (types `world_tombstone`, `world_portal`, `world_portal_paired`,
  `world_tame`, `world_base_new`, `world_base_grew`, `world_boss`), with ids
  derived from the save and the object so re-runs are idempotent.
- Backfill: derivation runs in the background (`CatchUpAsync`), both after
  ingest and once per server at startup, replaying every stored snapshot
  since 30 Sep in order, one at a time, resumable across a restart; see
  "Derivation" below for shutdown behaviour.
- Bases are matched one to one across saves, by nearest centre; growth
  below 25 pieces is not reported.

## Raids (agent)

`logwatch` parses `Random event set:<name>` into an `event_raid` event. This
ships with the player profile's agent update (restarts the game servers);
until then the Raids chip shows 0.

## API

- `GET /api/servers/{id}/activity?before=<rfc3339>&days=3` — events in
  `[before − days, before)`, newest first, plus counts per category over
  the same window and `earliest` (when tracking began).
- `GET /api/servers/{id}/sessions/today` — today's sessions per player in
  the server's local time, for "Who was on today".
- Both behind the same unlock check as the other server routes.

## Browser

- Desktop: "Full timeline →" in the side panel's Recent activity opens the
  Activity view in the panel with a back arrow; the panel's short activity
  list follows the same filters. Mobile: full-height sheet, from the main
  menu's "Full timeline". URL `#activity`; browser back closes it.
- Header "Activity", subtitle "Last 3 days · from the server log and world
  saves", "Reset filters".
- Who was on today: a row per player who played today on a 00–24 axis (local
  time); current session orange, earlier grey; "Now · HH:MM" line; legend.
- Show: eight chips with counts, toggling on and off. People: "Everyone" and
  a chip per player (initial); choosing players limits the list to their
  events, while server events stay.
- List grouped by day ("Today · Tue 29 Sep · in-game day 214", "Yesterday
  …"); each entry has time, icon, text and source line.
- End of list: "Show earlier" (loads 3 more days) or "Tracking began 30 Sep";
  "Nothing matches these filters" when filters hide everything; the design's
  "Where each entry comes from" footnote.

## Testing

- Go: each save-diff rule (including an unchanged save producing nothing, a
  portal pairing later, growth under 25 pieces), idempotent backfill, activity
  paging and counts, today's sessions across midnight and an open session,
  raid parsing.
- Web: Vitest for chip and people filtering and day grouping; Playwright on
  desktop and mobile: open, toggle chips, "Show earlier", "Show on map →",
  back.

## Rollout

Ships with the player profile. App first (timeline complete except raids);
the agent update adds raids together with the profile's portals and tames.

## Implementation notes (Plan 7)

Decisions the plan made where this spec left room:

- **Matching across saves:** marker and base ids are per-save rankings, so
  tombstones and portals match by owner or tag within 4 m, bases by the
  previous base whose centre is nearest (within its radius plus 32 m), and
  tames by counting (species, name): tames walk about. Unnamed tames (bred
  or freshly tamed animals) aren't reported. Owners and namers aren't part
  of any match, so the agent update that adds them reports nothing. Bases
  match one to one; when saves merge, a base's growth is measured against
  the sum of every base it absorbed.
- **Baseline:** the first stored save of a server yields no events; what is
  in it predates tracking.
- **Portals:** a new portal's event says whether it is paired already, and
  a pair that forms between two old portals gets its own "now paired"
  event. Either way, a portal counts as paired only when both ends —
  including, for a new portal's own event, the new portal itself — are
  explored; a pairing with an unexplored end is reported as unpaired (or
  not at all) until that end is explored. One pairing gives exactly one
  `world_portal_paired` event.
- **Retention:** only heartbeat and players_now events are pruned after 14
  days now; everything else stays, so the timeline reaches back to when
  tracking began.
- **Paging** is by the server's local days: without `before` the window ends
  at the end of today; the response's `from` is the next page's `before`.
  "Tracking began" shows once `from` reaches `earliest`.
- **Autosaves:** a run of autosaves with nothing between them is collapsed
  to its newest, in the API, before counting.
- **People** chips list every player the server has seen (online first) and
  are multi-select ("Everyone" clears them). Chip counts and "Who was on
  today" follow the people filter; the category chips don't change counts.
- **Raids:** the event names mapped to messages are the ones known from the
  game; anything else shows as its raw name ("Raid: army_gjall"). A raid's
  event id leaves the name out, so no other event's id changed.
- **Derivation** runs in the background, not inside the request: ingest
  starts it (`CatchUpAsync`, a failure is logged, not returned to the
  agent) and there's a second pass per server at startup, both serialised
  per server. Shutdown cancels the background context and waits, bounded,
  for in-flight derivation to actually return before the store closes.
- **The card's activity** uses the timeline's entry shape, so the side
  panel's short list follows the same filters; its world-save rows show
  "save HH:MM".
