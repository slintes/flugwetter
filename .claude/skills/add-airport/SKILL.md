---
name: add-airport
description: Add an airfield to flugwetter's airports.json, with its runway geometry and its published operating hours researched from the official AIP.
argument-hint: [ICAO or name ...]
when_to_use: Use when the user asks to add a new airport or airfield to this project.
user-invocable: true
---

Add an airfield to `internal/server/airports.json`.

**The research is `get-airport-info`** — invoke that skill first, with scope `all`, for every
airfield being added. It knows where each field comes from and how each source fails
silently; this skill is only about turning its report into an entry.

To re-check an airfield already in the file, use `refresh-airports` instead.

## What an entry looks like

```json
{
  "identifier": "EDWN",
  "name": "Nordhorn-Lingen",
  "latitude": 52.4575,
  "longitude": 7.185,
  "runways": ["05/23"],
  "runway_headings": [55.0, 235.0],
  "opening_hours": "SUM 0800-1800/SS+30; WIN 0800-1800/SS",
  "opening_hours_source": "AIP VFR AD 2-78, 12 DEC 2024",
  "website": "https://www.flugplatz-nordhorn-lingen.de/flugplatz.php?language=de"
}
```

`runways` and `runway_headings` run in the same order, all ends of every runway, headings
first-end-then-reciprocal per runway — Borkum's three runways are six numbers in the order
its `runways` array lists them.

`validateAirports` (`internal/server/airports.go`) is **fatal at startup**, not a warning.
It rejects: an empty or duplicate `identifier`, a missing `name`, out-of-range coordinates,
and an empty `runway_headings`. At most one airfield may be `pinned` and EDWN already is —
do not set it on a new entry.

The last three fields are optional. An airfield without them renders without that part of
the line; an airfield with a guessed one renders a confident wrong answer on a
flight-planning page. **Leave a field out rather than filling it from an unchecked source** —
if `get-airport-info` reported it unchecked, it is not sourced.

File position does not matter: `sortAirports` computes display order (pinned first, then
latitude descending). New entries go at the end of the file, where the last four went.

## When done

1. `make test`.
2. Run the server and check the airfield appears in the picker, its weather loads, and its
   hours line renders under the picker.
3. If the entry is long (Borkum's has four date-split ranges), check it wraps at 360px
   rather than overflowing.
