---
name: refresh-airports
description: Re-check the airfields already in flugwetter's airports.json against the current AIP and their websites, report what has drifted, and update the file once approved.
argument-hint: [ICAO ...] [--geometry]
when_to_use: Use when the user asks to refresh, re-check or update the stored airfield data, opening hours, AIP dates or website links.
user-invocable: true
---

Three fields in `internal/server/airports.json` go stale on their own and nothing in the
application notices: `opening_hours` (the AIP moves on a 28-day AIRAC cycle),
`opening_hours_source` (the stamp that makes that drift visible), and `website` (links rot).
This skill is the periodic check.

**The research is `get-airport-info`** — invoke that skill for the batch. This one decides
what to check, what changed, and what to write.

## Arguments

| argument | effect |
|---|---|
| *(none)* | every airfield in `airports.json` |
| one or more ICAOs | just those |
| `--geometry` | also re-derive coordinates and true headings |

`geometry` is off by default: runways rarely change, and Overpass rate-limits hard enough
that eighteen airfields is a background job with sleeps rather than a pass. Use it when
there is a reason to suspect a runway was renumbered, extended or closed.

## The run

1. Read `internal/server/airports.json`. That list is the work queue, in file order.
2. Invoke `get-airport-info` with scope `hours,website` — plus `geometry` when asked — for
   the whole batch at once, so it can keep one browser session for all of them.
3. Diff each result against the stored entry, one line per airfield:

   ```
   EDWJ Juist        hours unchanged   date 03 APR 2025 -> 28 MAY 2026   website 200
   EDWG Wangerooge   HOURS CHANGED     date 28 MAY 2026 (same)           website 200
   EDLH Hamm         hours unchanged   date unchanged                    WEBSITE 404
   EDWO Atterheide   UNCHECKED (AIP page did not load)                   website 200
   ```

   Show the old and new text in full for any airfield whose hours changed. That line is what
   the user is actually approving, and the AIP was read off an image.
4. **Stop and show the report before writing anything.** A wrong opening-hours line on a
   flight-planning page is worse than a stale one.
5. Apply what was approved, then `make test`, run the server and check the changed entries
   render — including the 360px wrap if a line grew.

## The date is a "verified against" stamp

When the TIME text is unchanged but the page now prints a newer date, **still rewrite
`opening_hours_source` to the newer date**. The stamp then means "checked against this
issue", which is what lets a later run tell a fresh entry from one nobody has looked at since
2024.

**That is rarer than it sounds.** The date in the AD page footer is when that page was last
amended, not the AIRAC cycle it was served in: in the 06 AUG 2026 cycle, Wangerooge's AD
2-106 still printed 28 MAY 2026 and Juist's AD 2-52 still printed 03 APR 2025. A page can
sit unchanged for years, so the ordinary outcome of a run is that nothing is written at all.
A run that reports no diff has still done its job — it has moved the entries from "assumed
current" to "checked".

## Unchecked is not unchanged

An airfield whose AIP page or website could not be reached is reported as **unchecked** and
its entry is left exactly as it was — including its old date. Never bump a stamp for an
airfield that was not actually read: a timeout that quietly confirms stale data is the one
outcome this skill exists to prevent.

Failures are worth retrying within the run — the AIP site and Overpass both fail
intermittently, as `get-airport-info` describes — but a persistent failure is reported, not
worked around.

## Committing

One commit for the whole run, naming what it was checked against, e.g.

```
chore(airports): refresh against the 06 AUG 2026 AIP cycle
```

If a website moved and the replacement is a judgement call (a redirect target, an operator's
page standing in for a dead airfield site), ask before storing it rather than deciding in the
diff.
