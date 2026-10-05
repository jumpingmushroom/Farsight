# Player profile

Status: approved design, 2026-10-05. Implements design section 08's player
profile (`design/World Atlas.dc.html` `:116-185` desktop aside, `:636-745`
mobile), which the MVP left out (DESIGN-NOTES §1.8).

## Goal

A "Profile →" link on every row in Online and Recently online opens a profile
for that player: playtime, the last 7 days, first and last seen, beds, bases,
portals they placed, tames they named, and deaths.

## Identity

- A player is a **platform ID** from the server log (`Steam_…`, PlayFab, …),
  as stored in the `sessions` table, shown under the latest name seen for it.
- Save data links to that player:
  - beds and tombstones by owner **name** (the save stores the name there);
  - bases by builder name (bases already list builders);
  - portals by **creator player ID**, resolved to a name through the save's
    player table (beds, tombstones);
  - tames by **namer platform ID** (`TamedNameAuthor`), which matches the
    log's platform ID directly.
- A name shared by two platform IDs links save data to both; this is rare
  and accepted.

## History

All history counts from when Farsight began recording (30 Sep 2026). "All
time" and "First seen" are labelled with a "tracked since …" note; nothing is
estimated.

## Data

| Field | Source |
|---|---|
| Status | live sessions: "Online now · 1h 12m" or "Last seen 2 h ago" |
| This week | session seconds in the last 7 days, the open session included |
| All time | all session seconds since tracking began |
| Sessions | count of sessions |
| Last 7 days | hours online per local day (server's log timezone), oldest first, today last; a session spanning midnight is split |
| First seen / last seen | earliest session start / latest session end (or "Online now") |
| Beds | count and nearest base name per bed, from the latest save |
| Bases | bases whose builders include the player, by their piece count: name, pieces, biome, position |
| Portals placed | portals whose creator is the player (tag, position) |
| Tames they named | tames whose namer is the player (name, species, position) |
| Deaths | distinct tombstones of theirs across all stored saves ("N spotted"), those first seen in the last 7 days ("M this week"), and their tombstones in the latest save with the save time they first appeared |

## API

`GET /api/servers/{id}/players/{playerId}` (behind the same unlock check as
the other server routes; 404 for an unknown player or locked server). The
tombstone history comes from the `tombstones` table, kept incrementally by
the world-save diff (see Implementation notes), not from scanning
snapshots.

## Agent

- Portal markers gain `owner` (creator ID resolved to a name).
- Tame markers gain `namer` (the `TamedNameAuthor` platform ID).

This needs an agent update, which restarts the game servers. The app ships
first; portals and tames show "None yet" until the agent is updated.

## Browser

- "Profile →" on Online and Recently online rows (design's ghost button,
  body font, `--cold-ink`).
- Desktop: the profile replaces the side panel's content (344 px box) with a
  back arrow to the server card. Mobile: the design's full-height sheet with
  "‹ PLAYER".
- The URL gets `#p=<playerId>` so browser back closes it and a profile can be
  linked.
- Layout as the design: avatar initial with online dot, name and status; three
  stat tiles; 7-day bar chart (hours, today in the accent colour); first seen,
  last seen, beds; Bases; Portals placed; Tames they named; Deaths; the
  design's source footnote, plus the "tracked since" note.
- "Map →" centres the map on the item (closing the sheet on mobile).
- States: loading skeleton, not found, "None yet." for empty sections.

## Testing

- Go: session maths (7-day buckets across midnight and the open session),
  linking by name, creator ID and namer ID, distinct tombstones across saves,
  the endpoint's unlock check and 404s.
- Web: Vitest for formatting and the chart model; Playwright opening a
  profile from Online and from Recently online on desktop and mobile, checking
  figures against seeded data, "Map →" and back.

## Rollout

1. Central app: profiles live except portals and tames.
2. Agent, at a quiet time: portals and tames fill in.

## Implementation notes (Plan 7)

Decisions the plan made where this spec left room:

- **Player id:** the sessions' `platformId` (no platform prefix) in the URL
  (`#p=`) and the API. A tame's namer (`TamedNameAuthor`, "Steam_…") is
  matched as `platform + "_" + platformId`; "host" names nobody.
- **Names:** save data links by every name the platform ID has played under,
  not only the latest.
- **Time zone:** each server in the config gets `timeZone` (IANA, default
  `UTC`), set to its agent's `FARSIGHT_LOG_TZ`; the app can't read the
  agent's environment.
- **"This week"** is the chart's seven local days (today and the six before),
  not a rolling 168 hours, so the tile and the bars agree. Deaths "this week"
  use the same days.
- **Explored only:** beds, bases, portals, tames and current tombstones are
  filtered to explored ground, like the snapshot API. A portal shows as
  paired only when its partner is explored. "N spotted" counts every
  tombstone.
- **Tombstone history** is kept incrementally: the world-save diff that
  writes timeline events also records every distinct tombstone, with the
  save it first appeared in, in a `tombstones` table. Stored snapshots are
  pruned after 14 days only once the world diff has passed them; a server
  with no `world_diff` row yet has nothing pruned. The table itself is never
  pruned.
- **Beds:** the nearest explored base within 300 m names a bed, otherwise
  its biome.
- **Tracked since** is the server's earliest stored event (the first session
  if there is none), shown as a note under first and last seen.
