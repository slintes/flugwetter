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

The alternative considered was a shared cost-per-severity table (one global "difficult
costs 20") with per-factor thresholds and a weight. It is smaller, but it forces every
factor onto the same cost scale, and these factors genuinely differ: a difficult ceiling
and a difficult gust spread are not worth the same. Each factor carries its own costs.

### D2 — Costs ramp between anchors instead of stepping

A factor's curve is a handful of `anchor{severity, at, cost}` points and the cost between
two of them is linearly interpolated. The gust spread that used to cost a flat 5 points for
crossing a line now costs a fraction of that, and a value just short of a hard limit costs
nearly what the limit does rather than nothing at all.

This is why the numbers in the table are not the numbers from the old ladder even where
the intent is unchanged: a ladder's penalty applied across a whole band, a curve's applies
at one point and grows into it.

### D3 — Direction is inferred, not declared

Cloud base and visibility get worse as they fall; wind, crosswind, rain and heat as they
rise. Rather than a `worse: higher|lower` field that can contradict the thresholds next to
it, the direction is read off the thresholds themselves. `init()` validates every curve —
at least two anchors, strictly monotone thresholds, strictly increasing costs, `perfect`
first and free, `noGo` last — and panics on a malformed table, the same way a malformed
`airports.json` is fatal at startup. A curve that runs backwards would otherwise score
every hour wrong in silence.

### D4 — Hard limits are anchors like any other, and any one of them ends the hour

The old ladder had three hard zeros written as early returns: ceiling, visibility and
civil twilight. They are now `noGo` anchors, most factors have one, and adding or removing
one is a line in the table rather than a branch in the evaluator.

The crosswind limit resolves a discrepancy that sat in `CLAUDE.md` for a while: the file
claimed a strong crosswind scored 0, the code only ever accumulated a penalty, and 20 kn
straight across the runway came out at 15%. There is a real limit now.

Both the steady crosswind and the gust spread carry their own limit. The spread's is not a
proxy for the peak — a wide spread means heavy gusting whatever the steady value is, and
that is worth calling off a flight for on its own.

### D5 — An anchor names the band that ends at it

A value is "difficult" from the moment it leaves the good anchor until it reaches the
difficult one. The exception is `noGo`, which names a wall rather than a band — the last
approach to it keeps the previous band's name, because calling a value that is nearly at
the limit "no-go" would claim the hour was already lost.

### D6 — The breakdown is part of the API

`scoreVFR` returns the factors that cost something alongside the score, worst first, and
`VfrPoint.Penalties` carries them to the browser, where the VFR chart's tooltip lists them.
An hour with nothing against it carries none, so a clear forecast adds nothing to the
payload. A no-go hour carries exactly one entry: the reason. Costs are rounded per factor
rather than at the end, so the numbers on screen add up to the score on screen.

Without this, "why is tomorrow 11:00 only 95%?" is answerable only by someone with the
source, the server and a debug log.

### D7 — Daylight is a factor like any other

It has no continuous scale — the twilight boundaries move with the date and the latitude,
so there is nothing constant to put in a threshold column. It is scored as an ordinal
(day, twilight, night) with the same anchor machinery: twilight costs a fixed penalty,
night is a no-go. Keeping it in the table is what makes the table complete; the alternative
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

## Calibration

**The numbers live in `vfrLimits` and nowhere else.** They are personal minima and get
retuned; a copy of them in a document is wrong by the next commit, which is why this record
describes the shape of the curves rather than their values.

What the shape is meant to express, and what should survive a retune:

- Total wind is the gentler of the two wind curves. It overlaps with crosswind, and a
  strong wind straight down the runway is not the problem a strong crosswind is.
- Gusts are scored on their margin over the steady crosswind, not their absolute value:
  5 gusting 15 is harder to land in than a steady 15.
- Precipitation is charged for what would fall, times how likely it is to fall. A 90% chance
  of a tenth of a millimetre is not a reason to stay on the ground; a 90% chance of a
  downpour is.
- A curve should not have a flat segment between two steep ones. A slope that rises,
  falls and rises again means the anchors disagree about where the difficulty is.

## Consequences

- Mid-band ceilings and visibilities cost more than they did under the ladder, because the
  ramp climbs toward 100 at the no-go instead of stepping.
- A factor without a no-go clamps at its last anchor rather than extrapolating; the old
  ladder multiplied without bound.
- An hour can still reach 0 by accumulation rather than by a no-go — poor visibility inside
  civil twilight will do it — and the breakdown says so.
- `CLAUDE.md` used to instruct that new rules be added inline to the one scoring function.
  That instruction is reversed: new rules are rows in `vfrLimits`, and a rule that cannot
  be expressed as a curve over one extracted value is a reason to reconsider the rule.
