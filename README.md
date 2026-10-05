# Farsight

A companion site for Valheim dedicated servers. It offers a world atlas rebuilt from each autosave, who's online, and how to join.

- **World atlas.** Terrain is generated from the world seed and rendered into map tiles. Markers for portals, beds, bases, tames, signs, boss altars, traders and dungeons are read from the latest autosave. The fog of war follows the game's own map: what has been recorded at a cartography table, plus 100 m around anything built. It is drawn into the map tiles on the server, so unexplored terrain never reaches the browser.
- **Server card.** Shows who's online and who was recently online, recent activity, the in-game day, bosses defeated, world rules, and the live crossplay join code.
- **Player profiles.** Playtime this week and since tracking began, hours online per day for the last 7 days, and each player's bases, beds, portals, named tames and deaths, linked from the save by who placed or named them.
- **Activity timeline.** Joins, leaves, restarts and raids from the server log, plus what changed between world saves: new tombstones, portals, tames and bases, bases that grew and bosses defeated. Filter it by kind and by player, back to when tracking began.
- **Private by default.** Each server is unlocked with a shared passphrase. Player positions are never shown.

Farsight has two parts:

- **`farsight-agent`** runs as a sidecar next to the Valheim server. It reads the world save and the server log, and pushes snapshots and events to the central app.
- **`farsight`** stores the pushed data (SQLite), renders map tiles, and serves the web UI and JSON API.

## Building

You need Go 1.27+ and Node 24+.

```sh
make web farsight-ui              # build the web UI and embed it in ./farsight
go build ./cmd/farsight-agent     # the sidecar
go test ./...                     # Go tests (no Node needed)
make test-web                     # web type-check and unit tests
make e2e                          # Playwright end-to-end tests
```

Without the `webui` build tag, `farsight` serves a placeholder page instead of the UI. Container images are built from `Dockerfile` and `Dockerfile.agent`.

Each server in `farsight.json` may set `timeZone`, an IANA name such as `"Europe/Oslo"` (default `"UTC"`). Set it to the same zone as that server's agent's `FARSIGHT_LOG_TZ`: profiles and the activity timeline count days in it.

## Not affiliated with Iron Gate

Farsight is an unofficial, non-commercial fan project. It is not affiliated with, endorsed by, or sponsored by Iron Gate AB or Coffee Stain Publishing. Valheim is a trademark of Iron Gate AB.

To draw a world's map and read its saves, Farsight reimplements some of Valheim's behaviour:

- the world generator (`internal/worldgen`);
- the save file formats (`internal/save`, `internal/zpkg`);
- the table of in-game object names (`internal/names`).

The MIT licence in [LICENSE](LICENSE) covers the Farsight code written for this project. It grants no rights in Valheim or in any work of Iron Gate AB. This repository contains no game files or decompiled game code, and it must never do so.

If you represent Iron Gate and would like any part of this removed, please open an issue and it will be taken down.

## Licence

[MIT](LICENSE)
