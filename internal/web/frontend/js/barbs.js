// Wind barb arithmetic, kept separate from the drawing so it can be tested without a canvas.

// Below this a barb is drawn as a circle rather than a shaft. The frontend is the only
// place this threshold exists -- the backend used to carry a second copy that nothing read.
export const CALM_THRESHOLD_KT = 3;

// barbComponents decomposes a speed into the standard aviation symbol parts: a pennant per
// 50kt, a full barb per remaining 10kt, and a half barb for a remaining 5kt. The speed is
// rounded first, so 47kt reads as 50 rather than as four barbs and a half.
export function barbComponents(speedKnots) {
    const speed = Math.round(speedKnots);
    return {
        pennants: Math.floor(speed / 50),
        fullBarbs: Math.floor((speed % 50) / 10),
        halfBarb: Math.floor((speed % 10) / 5),
    };
}

export function isCalm(speedKnots) {
    return speedKnots < CALM_THRESHOLD_KT;
}
