# Design: VFR scoring

Status: implemented (2026-08-06). Replaces the hand-written penalty ladder that lived in
`calculateVFRProbability` in `internal/server/weather.go`.

Goal: one place that states the limits, an evaluator that derives the score from it, and a
breakdown that says why an hour scored what it did.

## What prompted it

An August afternoon at EDWN scored 95% with nothing wrong with it: no ceiling, 41 km
visibility, no rain, 20.6 °C, 4.2 kn of wind. The one penalty was the crosswind gust rule.
The gusting crosswind was 7.27 kn against a 3.02 kn steady crosswind, a spread of 4.24 kn,
and the rule read:

```go
sigCrossWindGusts := int(crosswindGusts-crosswind) - 3
if sigCrossWindGusts > 0 {
    if sigCrossWindGusts > 7 { reduce = 20 } else if sigCrossWindGusts > 2 { reduce = 10 } else { reduce = 5 }
    probability -= reduce
}
```

A tenth of a knot less and the hour would have been 100%. The cliff was the symptom; the
shape of the code was the problem. ~170 lines of if/else with thresholds, multipliers and
penalties interleaved, no single place to read the limits off, every rule its own shape
(flat steps here, `n × excess` there, truncation to whole knots in some branches and raw
floats in others), and no way to explain a score without running the server under
`FLUGWETTER_LOG_LEVEL=debug`.

## Decisions

### D1 — One table of factors, in `internal/server/vfr.go`

`vfrLimits` is the single source of truth for every limit and every penalty. Retuning the
score is editing numbers in that table and nothing else.

**Revised.** This originally rejected a shared cost-per-severity table with per-factor
weights, on the grounds that a difficult ceiling and a difficult gust spread are not worth
the same. In practice the per-factor costs said the opposite of what they meant: "critical"
cost 20 in one factor and 40 in another, so the severity words were labels rather than
statements, and retuning meant arguing about forty numbers instead of eight.

The ladder is now shared — `severityCost`, one cost per severity — and a factor's `weight`
multiplies it. Where a factor reaches a severity is still entirely its own business, which
is where the real difference between a ceiling and a gust spread lives. A factor is four
thresholds and one weight.

**Revised again by D9.** The ladder itself is gone: severity is no longer converted to a
number at all, so there is nothing left to share and nothing left to weight.

### D2 — Costs ramp between anchors instead of stepping

A factor's curve is a handful of `anchor{severity, at}` points and the cost between two of
them is linearly interpolated, the ladder's cost at each end times the factor's weight. The gust spread that used to cost a flat 5 points for
crossing a line now costs a fraction of that, and a value just short of a hard limit costs
nearly what the limit does rather than nothing at all.

This is why the numbers in the table are not the numbers from the old ladder even where
the intent is unchanged: a ladder's penalty applied across a whole band, a curve's applies
at one point and grows into it.

### D3 — Direction is inferred, not declared

Cloud base and visibility get worse as they fall; wind, crosswind, rain and heat as they
rise. Rather than a `worse: higher|lower` field that can contradict the thresholds next to
it, the direction is read off the thresholds themselves. `init()` validates every curve —
at least two anchors, strictly monotone thresholds, strictly increasing severities,
`perfect` first, a positive weight, and no `noGo` anchor — and panics on a malformed table,
the same way a malformed
`airports.json` is fatal at startup. A curve that runs backwards would otherwise score
every hour wrong in silence.

### D4 — The last limit is the wall, and any one of them ends the hour

The old ladder had three hard zeros written as early returns: ceiling, visibility and
civil twilight. They are now a `wall` flag on the factor, most factors carry one, and
adding or removing one is a line in the table rather than a branch in the evaluator.

**Revised.** The wall was originally a fifth anchor beyond the critical one, with the cost
ramping from critical up to 100 across the gap. That made the last band the steepest and
least explicable part of the table — 7 km of visibility cost 65 points on a 5 km wall,
which is most of an hour for a value that is legal and flyable. The critical limit and the
wall are now one number: reaching it costs `critical`, passing it ends the hour. Five bands,
four thresholds.

The crosswind limit resolves a discrepancy that sat in `CLAUDE.md` for a while: the file
claimed a strong crosswind scored 0, the code only ever accumulated a penalty, and 20 kn
straight across the runway came out at 15%. There is a real limit now.

Both the steady crosswind and the gust spread carry their own limit. The spread's is not a
proxy for the peak — a wide spread means heavy gusting whatever the steady value is, and
that is worth calling off a flight for on its own.

**Revised by D10.** The gust spread's limit is gone; gusts no longer carry a wall or any
other entry in the table.

### D5 — An anchor names the band that ends at it

A value is "difficult" from the moment it leaves the good anchor until it reaches the
difficult one. There is no band named "no-go": the wall is a property of the factor, not a
point a value lands on, so the worst a scored hour can be called is critical.

### D6 — The breakdown is part of the API

`scoreVFR` returns the factors that cost something alongside the score, worst first, and
`VfrPoint.Penalties` carries them to the browser, where the VFR chart's tooltip lists them.
An hour with nothing against it carries none, so a clear forecast adds nothing to the
payload. A no-go hour carries exactly one entry: the reason. Costs are rounded per factor
rather than at the end, so the numbers on screen add up to the score on screen.

Without this, "why is tomorrow 11:00 only 95%?" is answerable only by someone with the
source, the server and a debug log.

**Revised by D9.** There is no score left to add up to, so costs are gone from the
breakdown along with the rest of the ladder. It still lists every non-perfect factor worst
first, and now does so even for a no-go hour: nothing is trimmed to keep an arithmetic total
intact, because there no longer is one.

### D7 — Daylight is a factor like any other

It has no continuous scale — the twilight boundaries move with the date and the latitude,
so there is nothing constant to put in a threshold column. It is scored as an ordinal
(day, twilight, night) with the same anchor machinery: one named band for twilight and the
wall at night, weighted below 1 because a legal-but-uncomfortable hour is not worth what a
critical crosswind is. Keeping it in the table is what makes the table complete; the alternative
was a special case in the evaluator, which is exactly what this design set out to remove.

Its value is meaningless to a reader, so it is the one factor with no unit, and the tooltip
formatter omits the number for any factor without one.

### D8 — Precipitation amount and probability are one penalty, not two

They started as two rows whose costs were added, with the probability row gated behind a
hard "only once there is at least this much rain" guard. That cannot express what actually
matters: the same probability is worth almost nothing against light rain and a great deal
against heavy rain, and an added penalty knows nothing about the amount it is added to. The
guard made it worse — near-certain light rain fell below the gate and its certainty was
discarded outright.

So a factor may now be **scaled**: its cost is what the value is worth if it happens, times
a weight for how likely that is. The amount curve stays in mm/h, which keeps its anchors
readable and tunable as ordinary rainfall rates.

Two things this deliberately is not:

- **Not a formula.** The weight is an interpolated curve like every other curve here, so the
  risk attitude is data. A straight line is the expected-cost reading — cost times chance,
  no attitude at all. The shipped weights are deliberately not straight: they are flat at
  the bottom, steep through the middle and flat again at the top, because that is what a
  go/no-go decision does. Below a certain chance the hour is not in question; above another
  it is decided; the middle is where the answer moves.
- **Not applicable to a no-go.** Scaling a hard limit would answer "does an unlikely deluge
  still end the hour" by side effect. `validate()` refuses a factor that carries both, so
  the question has to be answered on purpose. Precipitation's own no-go was dropped in the
  same change: its curve reaches a full 100 unaided, so certain heavy rain still ends the
  hour — by accumulation, with the breakdown naming the reason.

The mechanism generalises. Forecast gusts have the same problem — Open-Meteo's ensemble can
say how much the members agree about them — and that would be a scale on the gust factor
rather than anything new.

**Revised by D9.** The scale now discounts the *value* toward the curve's own perfect
anchor before the curve is read, rather than discounting a cost afterward — there is no
cost left to discount. The weight curve itself, its decision shape, and the "no wall on a
scaled factor" rule are all unchanged; only which side of the lookup the discount lands on
moved.

### D9 — Severity replaces points; the rating is the worst band, not a sum

**Revises D1, D6 and D8.** The shared ladder made retuning easier than the fully per-factor
costs it replaced, but it did not make the score easy to reason about: turning a handful of
independent judgements ("this crosswind is difficult") into one number still needed weights
to make severities comparable across factors, and the weights were themselves another layer
nobody could eyeball.

An hour's rating is now the worst severity any single factor reaches — a weakest-link read,
not a sum. `severityCost`, `factor.weight` and the cost arithmetic in `evaluate`/`scoreVFR`
are gone. Every curve's thresholds are unchanged; only the aggregation is different.

Two mechanisms carried over with their shape intact, re-pointed at a value instead of a
cost:

- **The wall.** `factor.wall` still makes the last anchor a no-go; passing it still ends
  the hour outright, whatever the rest of the weather is doing.
- **Scaling.** Precipitation's probability still discounts its severity, but by discounting
  the *value* toward the curve's own perfect anchor before the curve is read, rather than
  discounting a cost afterward: `effective = perfectAt + (v-perfectAt) * weight`. At
  weight 1 nothing changes; at weight 0 the value collapses onto the perfect anchor,
  whatever it actually was. `scale`/`scalePoint` and their validation are otherwise
  unchanged.

One behaviour changed as a direct consequence, not a separate choice: **every factor is now
evaluated, with no short-circuit on the first no-go.** Two factors can be past their wall in
the same hour, and the breakdown lists both — previously "the order of the table decides
which reason a doomed hour reports" was accepted as a quirk of the sum; removing the sum
removed the reason to accept it.

Twilight's anchor moved from `critical` (softened with a 0.5 weight, costing 25 of a
possible 50) to `difficult` outright. Without a weight to fake a smaller number, the honest
way to keep twilight from reading as bad as a critical crosswind is to give it the softer
band by name.

### D10 — Gusts leave the table; a separate warning replaces them

**Revises D4.** The hour that prompted this whole design — 3.02 kn steady crosswind gusting
7.27 kn, marked down for a 4.24 kn spread — was fixed by D2's curve, not solved: a gust
margin is still not something a good ceiling and clear visibility should be judged against
on the same scale as a critical crosswind. It measures something real (heavy gusting), but
whether it is worth grounding an otherwise-good hour over is a judgement call the pilot
should make with the actual numbers in front of them, not one the rating should make for
them by folding it into a severity.

So "crosswind gust spread" is gone from `vfrLimits` entirely — not retuned, removed — and
replaced with `gustWarning` in `internal/server/weather.go`: a plain boolean, judged on the
raw readings rather than a margin, wind gusts over 20 kn or crosswind gusts over 10 kn,
either one. It does not touch `conditions`, `scoreVFR`, or the rating in any way; it is
computed alongside scoring, not as part of it, and reaches the frontend as
`VfrPoint.GustWarning`, drawn as a small mark on the rating badge rather than a letter or a
color. An hour can be a perfect rating and still carry the mark.

The curves-not-steps shape D2 introduced is unaffected — this removes a factor, not the
mechanism that scores the rest of them.

## Calibration

**The numbers live in `vfrLimits` and nowhere else.** They are personal minima and get
retuned; a copy of them in a document is wrong by the next commit, which is why this record
describes the shape of the curves rather than their values.

What the shape is meant to express, and what should survive a retune:

- Total wind's thresholds sit further out than crosswind's. It overlaps with crosswind, and
  a strong wind straight down the runway is not the problem a strong crosswind is -- so wind
  reaches each band later, not "for less", since there is no longer a "less" to weigh it
  against.
- Precipitation is judged on what would fall, times how likely it is to fall. A 90% chance
  of a tenth of a millimetre is not a reason to stay on the ground; a 90% chance of a
  downpour is.
- A curve's thresholds should each mark a real change in difficulty. Two anchors placed so
  close together that the band between them is essentially never reached is a threshold
  that was not really chosen.

## Consequences

- An hour's rating is the worst band any factor reaches, not an accumulation of all of
  them: a critical crosswind alongside three merely-good factors rates exactly as badly as
  the crosswind would on its own. Nothing adds up any more, and that is the point.
- A factor without a wall clamps at its last anchor's severity instead of escalating
  further; precipitation is the case, so heavy but unlikely rain cannot end the hour by
  itself.
- Two factors can each be past their own wall in the same hour, and the breakdown lists
  both, worst first, rather than whichever the table happens to reach first.
- `CLAUDE.md` used to instruct that new rules be added inline to the one scoring function.
  That instruction is reversed: new rules are rows in `vfrLimits`, and a rule that cannot
  be expressed as a curve over one extracted value is a reason to reconsider the rule.
