// Airport selection: the dropdown, the shareable ?airport= URL, and the Leaflet map picker.
//
// This module does not import api.js. Selecting an airport has to trigger a reload, but
// having the picker call into the loader and the loader read the picker's state would make
// the two modules mutually dependent; main.js passes the reload in instead.

import { applyDensity, isLowDensity, isWideViewport } from './viewport.js';

// Airport selection state, filled from /api/config on startup.
let appConfig = { airports: [], default_airport: '', openaip_overlay: false };
let currentAirportId = '';

const AIRPORT_STORAGE_KEY = 'flugwetter.airport';

// Set by initAirportPicker; called whenever the selection changes.
let onAirportChange = () => {};

export function getCurrentAirportId() {
    return currentAirportId;
}

export function getAppConfig() {
    return appConfig;
}

// ---------------------------------------------------------------------------
// Airport selection
// ---------------------------------------------------------------------------

export async function initAirportPicker(onChange) {
    onAirportChange = onChange || onAirportChange;

    const select = document.getElementById('airportSelect');
    const mapButton = document.getElementById('mapPickerButton');

    try {
        const response = await fetch('/api/config');
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        appConfig = await response.json();
    } catch (error) {
        // Without a config there is nothing to select between. The charts still load the
        // backend's default airport, so the dashboard degrades to its pre-selector state.
        console.error('Error loading config:', error);
        select.disabled = true;
        mapButton.disabled = true;
        return;
    }

    // The list arrives in display order (pinned first, then north to south); it is kept
    // as-is so the ordering rule lives in exactly one place.
    select.innerHTML = '';
    appConfig.airports.forEach(airport => {
        const option = document.createElement('option');
        option.value = airport.identifier;
        option.textContent = airportLabel(airport);
        select.appendChild(option);
    });

    currentAirportId = initialAirportId();
    select.value = currentAirportId;
    updateAirportHeading();

    select.addEventListener('change', () => selectAirport(select.value));
    mapButton.addEventListener('click', openAirportMap);
    document.getElementById('mapModalClose').addEventListener('click', closeAirportMap);
    document.getElementById('airportMapModal').addEventListener('click', event => {
        // Click on the backdrop itself, not on the dialog inside it.
        if (event.target.id === 'airportMapModal') {
            closeAirportMap();
        }
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape') {
            closeAirportMap();
        }
    });
}

function airportLabel(airport) {
    const runways = airport.runways && airport.runways.length > 0
        ? ` (${airport.runways.join(', ')})`
        : '';
    return `${airport.identifier} — ${airport.name}${runways}`;
}

function findAirport(identifier) {
    return appConfig.airports.find(airport => airport.identifier === identifier);
}

// initialAirportId resolves the airport to show first: an explicit ?airport= wins so links
// are shareable, then the last choice made in this browser, then the backend default.
// An identifier that is no longer configured falls back instead of erroring.
function initialAirportId() {
    const fromUrl = new URLSearchParams(window.location.search).get('airport');
    if (fromUrl && findAirport(fromUrl)) {
        return fromUrl;
    }

    let stored = null;
    try {
        stored = localStorage.getItem(AIRPORT_STORAGE_KEY);
    } catch (error) {
        // Private mode / disabled storage. Not worth failing over.
    }
    if (stored && findAirport(stored)) {
        return stored;
    }

    if (findAirport(appConfig.default_airport)) {
        return appConfig.default_airport;
    }
    return appConfig.airports.length > 0 ? appConfig.airports[0].identifier : '';
}

function selectAirport(identifier) {
    if (!identifier || identifier === currentAirportId || !findAirport(identifier)) {
        return;
    }

    currentAirportId = identifier;

    const select = document.getElementById('airportSelect');
    if (select.value !== identifier) {
        select.value = identifier;
    }

    try {
        localStorage.setItem(AIRPORT_STORAGE_KEY, identifier);
    } catch (error) {
        // See initialAirportId: storage being unavailable is not fatal.
    }

    // Keep the URL shareable without adding a history entry per click.
    const url = new URL(window.location.href);
    url.searchParams.set('airport', identifier);
    window.history.replaceState({}, '', url);

    updateAirportHeading();
    updateMapSelection();
    onAirportChange();
}

// The heading itself stays plain "Flugwetter" -- the dropdown next to it already names the
// airport, so repeating it there only cost a line. The tab title carries the full
// identifier and name, because that is the only place the airport is visible when the page
// sits in a background tab, and tabs truncate from the right.
function updateAirportHeading() {
    const airport = findAirport(currentAirportId);
    document.title = airport
        ? `Flugwetter ${airport.identifier} — ${airport.name}`
        : 'Flugwetter - Aviation Weather Forecast';
    updateOpeningHours(airport);
}

// The airfield's published operating times, as the AIP prints them.
//
// Each of the three fields is optional and rendered only if present, so a partially filled
// entry still looks deliberate rather than showing an empty separator. The source carries a
// date because the AIP moves on a 28-day cycle and this JSON does not; the link is how a
// reader finds out the printed line has drifted.
function updateOpeningHours(airport) {
    const element = document.getElementById('openingHours');
    element.replaceChildren();

    const parts = [];
    if (airport && airport.opening_hours) {
        parts.push(document.createTextNode(airport.opening_hours));
    }
    if (airport && airport.opening_hours_source) {
        const source = document.createElement('span');
        source.className = 'opening-hours-source';
        source.textContent = airport.opening_hours_source;
        parts.push(source);
    }
    if (airport && airport.website) {
        const link = document.createElement('a');
        link.href = airport.website;
        link.target = '_blank';
        // noopener because target=_blank otherwise hands the new page a handle on this
        // one; noreferrer keeps the airfield's logs free of our URL.
        link.rel = 'noopener noreferrer';
        link.textContent = 'airfield site';
        parts.push(link);
    }

    parts.forEach((part, i) => {
        if (i > 0) {
            const separator = document.createElement('span');
            separator.className = 'opening-hours-separator';
            separator.textContent = '·';
            element.appendChild(separator);
        }
        element.appendChild(part);
    });

    element.hidden = parts.length === 0;
}

// ---------------------------------------------------------------------------
// Map picker
// ---------------------------------------------------------------------------

let airportMap = null;
let airportMarkers = {};

// Bootstrap view, only used until fitMapToAirports frames the real airports. Roughly
// north-west Germany.
const MAP_FALLBACK_VIEW = { center: [52.7, 7.5], zoom: 7 };

function openAirportMap() {
    const modal = document.getElementById('airportMapModal');
    modal.hidden = false;
    initAirportMap();
    // Leaflet measures the container on creation, and the container had no size while the
    // modal was hidden. Both the first open and every later one need the recalculation.
    setTimeout(() => {
        airportMap.invalidateSize();
        fitMapToAirports();
    }, 0);
}

function closeAirportMap() {
    // A popup left open would still be there on the next open, since the map instance is
    // reused rather than rebuilt.
    if (airportMap) {
        airportMap.closePopup();
    }
    document.getElementById('airportMapModal').hidden = true;
}

// The dialog is sized in CSS and follows the window, but Leaflet caches the pixel size of
// its container: without this the tile grid keeps the dimensions it had when the modal was
// opened, leaving grey gaps on the way out and clipped tiles on the way in. The view itself
// is left alone -- refitting would undo whatever the user panned to.
window.addEventListener('resize', () => {
    // Zooming the browser changes devicePixelRatio and fires resize.
    applyDensity();
    if (airportMap && !document.getElementById('airportMapModal').hidden) {
        airportMap.invalidateSize();
        // Re-applies the marker styles, which carry the viewport-dependent radius.
        updateMapSelection();
    }
});

function initAirportMap() {
    if (airportMap) {
        return;
    }

    // The view has to be set here, not later: Leaflet resolves a layer's tile coordinates
    // against the map's pixel origin the moment it is added, and that origin does not exist
    // until the map has a center and zoom. Creating the map bare and calling fitBounds
    // afterwards throws inside the first addTo(), leaving the modal on a blank grey box.
    airportMap = L.map('airportMap', {
        scrollWheelZoom: true,
        // Leaflet's default is one whole zoom level per 60px of wheel delta, and a single
        // notch on a normal mouse already delivers far more than that, so one flick jumped
        // several levels. 200px per level plus quarter-level snapping turns the wheel into
        // a gradual zoom. The +/- buttons keep their full-level step (zoomDelta default).
        wheelPxPerZoomLevel: 200,
        zoomSnap: 0.25,
        center: MAP_FALLBACK_VIEW.center,
        zoom: MAP_FALLBACK_VIEW.zoom
    });

    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
        maxZoom: 14,
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>'
    }).addTo(airportMap);

    const note = document.getElementById('mapOverlayNote');
    if (appConfig.openaip_overlay) {
        // Proxied through the backend so the openAIP API key stays server-side.
        L.tileLayer('/api/tiles/openaip/{z}/{x}/{y}.png', {
            maxZoom: 14,
            attribution: 'Aeronautical data &copy; <a href="https://www.openaip.net">openAIP</a> (CC BY-NC 4.0)'
        }).addTo(airportMap);
        note.textContent = '';
    } else {
        note.textContent = 'openAIP overlay unavailable — set OPENAIP_API_KEY on the server to show airspaces.';
    }

    appConfig.airports.forEach(airport => {
        const marker = L.circleMarker([airport.latitude, airport.longitude], markerStyle(false))
            .addTo(airportMap)
            .bindTooltip(airport.identifier, {
                permanent: true,
                direction: 'right',
                className: 'airport-tooltip',
                offset: [tooltipOffsetX(), 0]
            });

        // Details on hover, not on click. bindPopup() would open on click -- the same click
        // that selects the airport and closes the modal -- so the popup flashed on the way
        // out and was still open the next time the map was shown. Opening it by hand keeps
        // click free for selection, and openOn() closes any previously open popup.
        marker.on('mouseover', () => {
            L.popup({ closeButton: false, autoPan: false, offset: [0, -4] })
                .setLatLng([airport.latitude, airport.longitude])
                .setContent(airportPopup(airport))
                .openOn(airportMap);
        });
        marker.on('mouseout', () => airportMap.closePopup());

        marker.on('click', () => {
            selectAirport(airport.identifier);
            closeAirportMap();
        });

        airportMarkers[airport.identifier] = marker;
    });

    updateMapSelection();
}

// Marker radii. Leaflet takes these in CSS pixels, so without a step here a 2560px screen
// gets the same 7px dot as a 1280px one and it reads as undersized.
const MARKER_RADII = { normal: 7, selected: 10 };
const MARKER_RADII_WIDE = { normal: 9, selected: 13 };
// Multiples of 4 so the diameter stays a whole number of physical pixels at dpr 0.75.
const MARKER_RADII_LOW_DENSITY = { normal: 12, selected: 16 };

function markerRadii() {
    if (isLowDensity()) {
        return MARKER_RADII_LOW_DENSITY;
    }
    return isWideViewport() ? MARKER_RADII_WIDE : MARKER_RADII;
}

// Leaflet anchors a path's tooltip at the marker's centre, not its edge, so the label has
// to be pushed clear of the circle by hand — otherwise a larger radius grows out over it.
// The offset uses the *selected* radius for every marker, which is the largest a marker can
// get, so the labels stay in a straight line as the selection moves between them.
function tooltipOffsetX() {
    return markerRadii().selected + 6;
}

function markerStyle(selected) {
    const radii = markerRadii();
    return {
        radius: selected ? radii.selected : radii.normal,
        color: '#ffffff',
        weight: 2,
        fillColor: selected ? '#d63031' : '#0984e3',
        fillOpacity: 1
    };
}

function airportPopup(airport) {
    const runways = airport.runways && airport.runways.length > 0
        ? `<br>Runway ${airport.runways.join(', ')}`
        : '';
    return `<strong>${airport.identifier}</strong><br>${airport.name}${runways}`;
}

function updateMapSelection() {
    const offset = tooltipOffsetX();
    Object.keys(airportMarkers).forEach(identifier => {
        const marker = airportMarkers[identifier];
        marker.setStyle(markerStyle(identifier === currentAirportId));

        // The offset is fixed at bind time, so a density or viewport change that alters the
        // radius would otherwise leave the label sitting on top of the circle.
        const tooltip = marker.getTooltip();
        if (tooltip && tooltip.options.offset[0] !== offset) {
            tooltip.options.offset = L.point(offset, 0);
            marker.closeTooltip();
            marker.openTooltip();
        }
    });
}

// fitMapToAirports frames whatever airports were configured, so changing the list moves the
// map without a hardcoded viewport here.
function fitMapToAirports() {
    const points = appConfig.airports.map(airport => [airport.latitude, airport.longitude]);
    if (points.length === 0) {
        airportMap.setView(MAP_FALLBACK_VIEW.center, MAP_FALLBACK_VIEW.zoom);
        return;
    }
    airportMap.fitBounds(L.latLngBounds(points), { padding: [40, 40] });
}
