---
name: add-airport
description: Add an airfield to flugwetter's airports.json, with its runway geometry and its published operating hours researched from the official AIP.
argument-hint: [ICAO]
when_to_use: Use when the user asks to add a new airport or airfield to this project, or to check or refresh an existing entry's opening hours.
user-invocable: true
---

Add an airfield to `internal/server/airports.json`. Most of this is research, and most of
the research has a way of going quietly wrong — the notes below are the failure modes, not
general advice.

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

`validateAirports` (`internal/server/airports.go`) is **fatal at startup**, not a warning.
It rejects: an empty or duplicate `identifier`, a missing `name`, out-of-range coordinates,
and an empty `runway_headings`. At most one airfield may be `pinned` and EDWN already is —
do not set it on a new entry.

The last three fields are optional. An airfield without them renders without that part of
the line; an airfield with a guessed one renders a confident wrong answer on a
flight-planning page. Leave it out if you cannot source it.

File position does not matter: `sortAirports` computes display order (pinned first, then
latitude descending).

## Runway headings — the one that fails silently

`runway_headings` are **TRUE headings taken from OpenStreetMap runway geometry**, both ends
of every runway. They are *not* the published designators, which are magnetic and rounded to
10°: EDWN's "05/23" is true 55/235, not 50/230.

`crosswindComponent` takes the most favourable heading, so a multi-runway field needs all
ends listed. Getting this wrong breaks nothing and fails no test — every crosswind penalty
for that airfield is just quietly biased.

`runways` is the published designator, for display only.

### Getting the geometry: Overpass

Overpass is the query API for OpenStreetMap data (`https://overpass-api.de/api/interpreter`,
POST the query as the `data` parameter). Two queries per airfield:

```
[out:json][timeout:60];nwr["icao"="EDWZ"]["aeroway"="aerodrome"];out center tags;
[out:json][timeout:60];way(around:2500,53.72485,7.37315)["aeroway"="runway"];out geom tags;
```

The first gives `latitude`/`longitude`; the second gives each runway as a node list, plus a
`ref` tag that usually carries the designator. Take the true initial bearing from the first
node to the last, and its reciprocal for the other end:

```python
y = sin(dlon) * cos(lat2)
x = cos(lat1) * sin(lat2) - sin(lat1) * cos(lat2) * cos(dlon)
bearing = (degrees(atan2(y, x)) + 360) % 360
```

**It is rate-limited** — HTTP 429 after two airfields in quick succession, and the backoff
outlasts a foreground command's timeout. Run it in the background with sleeps between
queries and poll the output file, rather than in a blocking call.

Sanity-check the result against the designator: they should agree to within about 10°, since
the designator is the magnetic heading rounded. Langeoog's "05/23" came out as true
54.9/234.9 — 5° off, which is the magnetic variation and exactly why the designator is not
usable directly. A result tens of degrees away means the wrong way was matched.

If OSM returns two nearly-parallel ways for what the AIP calls one runway (Uetersen has a
second grass way 3° off the ref'd one), it is the same strip digitised twice or a parallel
glider strip. Use the `ref`'d one; the difference is immaterial to a crosswind.

## Opening hours: read them from the AIP

The DFS AIP VFR is the authoritative source and it is worth the trouble. When this project
first sourced hours from airfield websites and aggregators, the AIP later corrected four of
thirteen entries and supplied one that no website had at all.

It cannot be scraped. Each step below has a specific way of failing:

1. Open `https://aip.dfs.de/BasicVFR/` in a **real browser** (Playwright — see
   "Browser Access" in the global CLAUDE.md). `WebFetch` cannot do this.
2. The search input `#searchAirfield` exists but is **hidden**. Click
   `a[href='#popupSearch']` first, or the fill times out after 30s.
3. Type the ICAO, wait ~2.5s for the suggestions to populate, then click the right
   suggestion — **and "contains the ICAO" is not the right test.** Searching `EDHE` and
   selecting with `:has-text('EDHE')` lands on **Oedheim EDGO**, because "O*edhe*im"
   contains the string. The only symptom is that the page then has no `AD 2-` link; had
   Oedheim happened to have one, this would have produced a whole entry of the wrong
   airfield's data with nothing to flag it.

   Search by **name** and match the ICAO as a trailing token:

   ```js
   [...document.querySelectorAll('.dropdown-menu a, .dropdown-item')]
       .findIndex(a => /\bEDHE$/.test((a.textContent || '').trim()))
   ```

   The suggestion text is `Uetersen/Heist EDHE`, so the ICAO is always last. This also
   resolves the ICAO for you when you only know the name.
4. On the airfield's chapter page, follow the link whose text starts with **`AD 2-`**. The
   other links (`<ICAO> <name> 1`, `2`, `3`) are the visual approach charts.
5. **That page is a base64-embedded PNG, not text.** `document.body.innerText` is ~200
   characters of navigation furniture and every text selector matches nothing. Screenshot
   the `img[src^='data:image']` element and read the image.
6. The AD 2 pages list several airfields alphabetically — read the block under the right
   ICAO, not the neighbour above it.

Copy the `TIME` block **verbatim** into `opening_hours`. Do not paraphrase, do not convert,
do not tidy. Every token means something:

| token | meaning |
|---|---|
| `SUM` / `WIN` | summer / winter, the daylight-saving period |
| `SS+30`, `SR-30` | sunset plus 30 min, sunrise minus 30 min |
| `ECET` | end of civil evening twilight |
| `MAX SS` | closes at sunset if that comes first |
| `LDG-1100` | landings permitted until, later than departures |
| `O/T PPR` | other times by prior permission |
| `H 24` | around the clock |
| `0500 (0400)` | the bracketed value is the **summer** time |

Record the AD page number and the date printed on the page into `opening_hours_source`,
e.g. `"AIP VFR AD 2-78, 12 DEC 2024"`. The AIP moves on a 28-day AIRAC cycle and this JSON
does not — the date is what makes that drift visible instead of silent.

## Times are UTC

The AIP publishes UTC throughout, so the stored string carries no unit and the reader
applies the aviation default.

Airfield websites vary. `LT` means CET or CEST. A website in LT should equal the AIP value
plus one hour in winter and two in summer — a useful cross-check:

- Norderney's site says `08:00-19:00 LT`; the AIP says `0600-1700`. Consistent.
- Telgte's site says `09:00-20:00`; the AIP says `0700-1800`. Consistent.

If it does not reconcile, **the AIP wins**. Note the discrepancy rather than averaging it.

## The website link

Prefer the page that actually carries the hours over the homepage — Wangerooge publishes an
*Öffnungszeiten* page, Leer-Papenburg a *Betriebsinformationen* one. Otherwise the homepage
is fine.

Check the URL returns 200 and store it **after** any redirect (`edwj.de` →
`flughafen-juist.de`, `edwc.de` → `flugplatz-damme.de`). Some AIP entries list the
airfield's own address, which is the most reliable place to find it.

A `curl` failure is not proof a site is down: `flugplatz-hamm.de` fails TLS from this
sandbox and loads fine in a browser. Confirm in a browser before discarding a URL — and
equally, confirm before *keeping* one. Two failure modes seen so far:

- **Genuinely dead.** `flugplatz.langeoog.de` is the URL every source cites for Langeoog and
  it resolves nowhere. The island's own `Flugplatz Langeoog` page was used instead — the
  operator's site is a good fallback when the airfield's own has lapsed.
- **Broken certificate.** `edhe.de` serves a certificate for another name, so HTTPS fails
  while HTTP redirects fine to the real host. Ask before downgrading a stored link to
  `http://` or pointing it at the redirect target; which one is wanted is a judgement call
  about what the reader should see in the URL bar, not a technical one.

## When done

1. `make test`.
2. Run the server and check the airfield appears in the picker, its weather loads, and its
   hours line renders under the picker.
3. If the entry is long (Norderney's has four seasonal ranges), check it wraps at 360px
   rather than overflowing.
