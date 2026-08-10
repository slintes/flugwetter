package server

import (
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Restricted airspace activity, from the DFS airspace use plan.
//
// ED-R 37A contains EDWN, and the AIP requires permission before entering it. Its
// activation is published in the AUP, which is otherwise queried by hand at
// ais.dfs.de/pilotservice/briefing/aup.
//
// The endpoint below is what that page's form posts to. It is undocumented, takes no
// credential of any kind, and answers with HTML -- but HTML built for machines: the area
// name, the polygon and ISO-8601 timestamps are all in attributes, so this parses attributes
// rather than scraping prose.
//
// Two things about the data, both of which the UI has to respect:
//
//   - DFS states on that page that the plan covers only activations below FL100, "may not
//     be complete", and that AIP ENR 5.1 and NOTAM remain authoritative. The frontend shows
//     that disclaimer; this file must not pretend otherwise.
//   - The horizon is roughly a day. Beyond it the plan simply has not been published, which
//     is not the same as "not active", and an empty result must never be served as if it
//     were "nothing is happening".
const (
	aupURL = "https://ais.dfs.de/pilotservice/briefing/aup/ajax/aup_briefing.jsp"

	// The AUP is published about a day ahead, so anything shorter than this wastes nothing
	// and anything longer risks missing a same-day amendment by half a working day.
	restrictionsPollInterval = 6 * time.Hour

	// How far ahead to ask. The forecast is seven days; the plan will cover one or two of
	// them, and asking for the rest costs nothing.
	restrictionsHorizonDays = 7

	// Ask only for airspace reaching below 8000 ft. This does not truncate the limits that
	// come back -- an area from A055 to F350 still appears, because it affects you below
	// 8000 ft -- it drops the ones lying wholly above, which are none of this app's
	// business. Roughly a third of the volume.
	restrictionsUpperLimit = "A080"

	// One failed poll is a blip. Two in a row is a pattern worth showing, the same
	// threshold and for the same reason as the model-run poller.
	restrictionsFailuresBeforeDegraded = 2
)

type restrictionTracker struct {
	mutex sync.RWMutex

	areas            []RestrictedArea
	fetchedAt        time.Time
	consecutiveFails int
}

var restrictions = &restrictionTracker{}

// snapshot hands out a clone. RestrictedArea holds slices, and handing out the tracker's own
// would let a later poll rewrite what a request is midway through encoding -- the bug
// modelRunTracker.snapshot already documents, with more slices to get wrong.
func (t *restrictionTracker) snapshot() (areas []RestrictedArea, fetchedAt time.Time, degraded bool) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	cloned := make([]RestrictedArea, len(t.areas))
	for i, area := range t.areas {
		cloned[i] = RestrictedArea{
			Name:    area.Name,
			Windows: slices.Clone(area.Windows),
			Polygon: slices.Clone(area.Polygon),
		}
	}
	return cloned, t.fetchedAt, t.consecutiveFails >= restrictionsFailuresBeforeDegraded
}

// windowsFor returns one area's activity windows, or nil. Used by the frontend contract
// tests; the frontend itself filters the payload.
func (t *restrictionTracker) windowsFor(name string) []RestrictionWindow {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	for _, area := range t.areas {
		if area.Name == name {
			return slices.Clone(area.Windows)
		}
	}
	return nil
}

// poll fetches the plan and replaces the set. A failure leaves the previous set in place:
// stale activity times are useful, an empty list would read as "nothing is active".
func (t *restrictionTracker) poll(ctx context.Context) {
	now := time.Now().UTC()
	body, err := fetchAUPFn(ctx, now, now.AddDate(0, 0, restrictionsHorizonDays))
	if err != nil {
		t.mutex.Lock()
		t.consecutiveFails++
		fails := t.consecutiveFails
		t.mutex.Unlock()
		slog.Warn("airspace use plan unavailable", "error", err, "consecutive", fails)
		return
	}

	areas := parseAUP(body)

	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.areas = areas
	t.fetchedAt = time.Now().UTC()
	t.consecutiveFails = 0
	slog.Info("airspace use plan fetched", "areas", len(areas))
}

// fetchAUPFn indirects the network call so tests can stub it.
var fetchAUPFn = fetchAUP

func fetchAUP(ctx context.Context, from, to time.Time) (string, error) {
	form := url.Values{
		"DATE_BEGIN": {from.Format("2006-01-02")},
		"TIME_BEGIN": {"00:00"},
		"DATE_END":   {to.Format("2006-01-02")},
		"TIME_END":   {"23:59"},
		"LOWER":      {"GND"},
		"UPPER":      {restrictionsUpperLimit},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aupURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AUP returned status code: %d", resp.StatusCode)
	}

	page, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}
	return string(page), nil
}

var (
	// One <table class="airspace" data-part="ED-R37A" data-polygon="..."> per area.
	aupAreaRe     = regexp.MustCompile(`(?s)<table class="airspace"([^>]*)>(.*?)</table>`)
	aupAttrRe     = regexp.MustCompile(`data-(part|polygon)="([^"]*)"`)
	aupValidityRe = regexp.MustCompile(`(?s)<tr class="validity">(.*?)</tr>`)
	aupTimeRe     = regexp.MustCompile(`datetime="([^"]+)"`)
	aupLimitsRe   = regexp.MustCompile(`Lower Limit (\S+) to Upper Limit (\S+)`)
	aupTagRe      = regexp.MustCompile(`<[^>]*>`)
	// 522607N0072010E -- degrees, minutes, seconds, hemisphere, for each of lat and lon.
	aupPointRe = regexp.MustCompile(`^(\d{2})(\d{2})(\d{2})([NS])(\d{3})(\d{2})(\d{2})([EW])$`)
)

// parseAUP pulls the areas out of the briefing.
//
// Anything unparseable is skipped rather than failing the batch: this is one undocumented
// endpoint's HTML, and losing one malformed area is much better than losing the plan.
func parseAUP(page string) []RestrictedArea {
	var areas []RestrictedArea

	for _, match := range aupAreaRe.FindAllStringSubmatch(page, -1) {
		attrs, body := match[1], match[2]

		var name, polygon string
		for _, attr := range aupAttrRe.FindAllStringSubmatch(attrs, -1) {
			switch attr[1] {
			case "part":
				name = attr[2]
			case "polygon":
				polygon = attr[2]
			}
		}
		if name == "" {
			continue
		}

		area := RestrictedArea{Name: name, Polygon: parseAUPPolygon(polygon)}
		for _, row := range aupValidityRe.FindAllStringSubmatch(body, -1) {
			if window, ok := parseAUPWindow(row[1]); ok {
				area.Windows = append(area.Windows, window)
			}
		}
		if len(area.Windows) == 0 {
			continue
		}

		// The same area appears under each FIR that borders it; merge rather than repeat.
		if i := slices.IndexFunc(areas, func(a RestrictedArea) bool { return a.Name == name }); i >= 0 {
			for _, window := range area.Windows {
				if !slices.Contains(areas[i].Windows, window) {
					areas[i].Windows = append(areas[i].Windows, window)
				}
			}
			continue
		}
		areas = append(areas, area)
	}

	return areas
}

func parseAUPWindow(row string) (RestrictionWindow, bool) {
	times := aupTimeRe.FindAllStringSubmatch(row, -1)
	if len(times) < 2 {
		return RestrictionWindow{}, false
	}

	// "2026-08-11T07:00Z" -- ISO 8601 without seconds, which RFC3339 does not accept.
	const layout = "2006-01-02T15:04Z"
	from, err := time.Parse(layout, times[0][1])
	if err != nil {
		return RestrictionWindow{}, false
	}
	to, err := time.Parse(layout, times[1][1])
	if err != nil || !to.After(from) {
		return RestrictionWindow{}, false
	}

	window := RestrictionWindow{From: from, To: to}
	if limits := aupLimitsRe.FindStringSubmatch(html.UnescapeString(aupTagRe.ReplaceAllString(row, " "))); limits != nil {
		window.Lower, window.Upper = limits[1], limits[2]
	}
	return window, true
}

// parseAUPPolygon converts the dash-separated DMS points to decimal degrees.
func parseAUPPolygon(polygon string) [][2]float64 {
	if polygon == "" {
		return nil
	}

	var out [][2]float64
	for _, token := range strings.Split(polygon, "-") {
		m := aupPointRe.FindStringSubmatch(token)
		if m == nil {
			continue
		}
		lat := dmsDegrees(m[1], m[2], m[3])
		lon := dmsDegrees(m[5], m[6], m[7])
		if m[4] == "S" {
			lat = -lat
		}
		if m[8] == "W" {
			lon = -lon
		}
		out = append(out, [2]float64{lat, lon})
	}
	return out
}

func dmsDegrees(d, m, s string) float64 {
	deg, _ := strconv.Atoi(d)
	min, _ := strconv.Atoi(m)
	sec, _ := strconv.Atoi(s)
	return float64(deg) + float64(min)/60 + float64(sec)/3600
}

// watchRestrictions polls until ctx is cancelled.
//
// Nothing here invalidates any cache: the plan is served straight from this tracker, and the
// frontend picks it up on its next forecast reload. That is what makes the six-hour cadence
// enough without a refresh trigger of its own.
func watchRestrictions(ctx context.Context) {
	ticker := time.NewTicker(restrictionsPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			restrictions.poll(ctx)
		}
	}
}
