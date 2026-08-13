---
name: get-airport-info
description: Research one or more airfields — true runway headings from OpenStreetMap, published operating hours from the official DFS AIP, and a working website link. Reports the findings and writes nothing.
argument-hint: [ICAO or name ...] [scope: geometry|hours|website|all]
when_to_use: Use when the user asks what an airfield's opening hours, runways or website are, and as the research step of add-airport and refresh-airports.
user-invocable: true
---

Find out what is true about an airfield right now. **This skill writes no files** — it
reports, and the caller decides what to do with the answer.

Most of this is research, and most of the research has a way of going quietly wrong — the
notes below are the failure modes, not general advice.

## Scope

The caller says which parts it needs; default to all three when nothing is said.

| scope | what it costs | what it answers |
|---|---|---|
| `geometry` | two Overpass queries per airfield, rate-limited | `latitude`, `longitude`, `runways`, `runway_headings` |
| `hours` | one browser pass and one image read per airfield | `opening_hours`, `opening_hours_source` |
| `website` | one `curl`, sometimes a browser check | `website` |

Do not research what was not asked for. A refresh over eighteen airfields skips `geometry`
on purpose, because runways change rarely and Overpass rate-limits hard.

## What to report

One block per airfield, and nothing written to disk:

```
EDWR Borkum
  latitude/longitude : 53.59583, 6.7121                             [geometry]
  runways            : 13/31, 05/23, 12/30                          [geometry]
  runway_headings    : 130.0, 310.0, 46.3, 226.3, 118.4, 298.4      [geometry]
  opening_hours      : SUM until 30 SEP 0530-1700, O/T PPR; ...     [hours]
  source             : AIP VFR AD 2-19, 28 MAY 2026                 [hours]
  website            : https://stadtwerke-borkum.de/flugplatz/ (200, no redirect)
  notes              : site says "Öffnungszeiten siehe AIP"
```

**A field that could not be researched is reported as unchecked, never as unchanged or
absent.** A timeout that reads as "no hours published" is how a stale entry gets confirmed as
current.

## Batching

For more than two or three airfields, **keep one browser session for the whole batch**. The
AIP flow below is search → AD link → screenshot, and relaunching Playwright per airfield pays
the startup cost every time. One script, one browser, one PNG per airfield, then read the
images.

The AD 2 pages carry several airfields alphabetically, so two airfields in a batch can share
one page — dedupe by AD page number and read that screenshot once.

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

**It also just fails.** HTTP 504 from a healthy query, twice in a row for Borkum, then a 200
for the identical query minutes later — the public instance is shared and overloaded, which
looks nothing like the 429 and needs no backoff, only another attempt. Write the fetch as a
retry loop rather than a single call, and treat "no `elements` key" as a failure too: a 200
carrying an Overpass error page parses as JSON and then yields no runways.

```bash
for host in https://overpass-api.de/api/interpreter https://overpass.kumi.systems/api/interpreter; do
  for try in 1 2 3; do
    code=$(curl -s -o "$out" -w '%{http_code}' --max-time 150 "$host" --data-urlencode "data=$query")
    if [ "$code" = "200" ] && grep -q '"elements"' "$out"; then exit 0; fi
    sleep 40
  done
done
```

`overpass.kumi.systems` is a drop-in mirror, same query syntax, and worth having in the loop
as the second host — it was not needed for Borkum, but the main instance failing three times
running is not a scenario worth discovering by hand.

Sanity-check the result against the designator: they should agree to within about 10°, since
the designator is the magnetic heading rounded. Langeoog's "05/23" came out as true
54.9/234.9 — 5° off, which is the magnetic variation and exactly why the designator is not
usable directly. A result tens of degrees away means the wrong way was matched.

If OSM returns two nearly-parallel ways for what the AIP calls one runway (Uetersen has a
second grass way 3° off the ref'd one), it is the same strip digitised twice or a parallel
glider strip. Use the `ref`'d one; the difference is immaterial to a crosswind.

The AIP's own visual operation chart (`<ICAO> <name> 1`, alongside the AD 2 link) draws every
published runway and is the check on whether OSM's list is the right one — Borkum's three
came back from OSM and the chart confirmed all three, including the two grass strips.

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
   resolves the ICAO for you when you only know the name. Searching by name can return more
   than one hit — "Borkum" offers `Borkum EDWR` and `Borkum Inselkrhs.`, a hospital heliport
   — which the trailing-token match settles.
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

The AIP prints the block bilingually and over several lines; the stored form is one line,
with the German half of `bis/until` and `ab/from` dropped and the seasons separated by
semicolons. Borkum's four date-split ranges are the longest example in the file.

Record the AD page number and the date printed on the page into `opening_hours_source`,
e.g. `"AIP VFR AD 2-78, 12 DEC 2024"`. The AIP moves on a 28-day AIRAC cycle and this JSON
does not — the date is what makes that drift visible instead of silent, so read it off the
page footer every time rather than carrying the old one forward.

That footer date is when the **page** was last amended, not the cycle it is being served in.
The 06 AUG 2026 cycle still served Juist's AD 2-52 printed 03 APR 2025. So a page date that
has not moved is the normal case and means the hours genuinely have not been reissued — it
is not evidence that the wrong page was read.

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
is fine. Some sites decline to publish hours at all and point at the AIP (Borkum's does);
that is worth reporting as a note, and is not a reason to keep looking.

Check the URL returns 200 and report it **after** any redirect (`edwj.de` →
`flughafen-juist.de`, `edwc.de` → `flugplatz-damme.de`). Some AIP entries list the
airfield's own address, which is the most reliable place to find it.

A `curl` failure is not proof a site is down: `flugplatz-hamm.de` fails TLS from this
sandbox and loads fine in a browser. Confirm in a browser before discarding a URL — and
equally, confirm before *keeping* one. Two failure modes seen so far:

- **Genuinely dead.** `flugplatz.langeoog.de` is the URL every source cites for Langeoog and
  it resolves nowhere. The island's own `Flugplatz Langeoog` page was used instead — the
  operator's site is a good fallback when the airfield's own has lapsed.
- **Broken certificate.** `edhe.de` serves a certificate for another name, so HTTPS fails
  while HTTP redirects fine to the real host. Ask before recommending a link be downgraded to
  `http://` or pointed at the redirect target; which one is wanted is a judgement call about
  what the reader should see in the URL bar, not a technical one.
