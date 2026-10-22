// Node >= 25 exposes an unusable `localStorage` accessor on globalThis and
// vitest 4 does not overwrite pre-existing global keys when populating the
// jsdom environment. As a result `globalThis.localStorage` is undefined while
// `jsdom.window.localStorage` is a working Storage instance.
type WithJsdom = typeof globalThis & {
  jsdom?: { window: Window }
}

// Unchecked cast: vitest attaches the jsdom window at runtime; this is a
// library boundary the type system cannot express.
const g = globalThis as unknown as WithJsdom
const jsdomWindow = g.jsdom?.window

if (jsdomWindow && typeof globalThis.localStorage === 'undefined') {
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: jsdomWindow.localStorage,
  })
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    value: jsdomWindow.sessionStorage,
  })
}