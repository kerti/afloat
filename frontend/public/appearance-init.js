/*
 * Applies the cached appearance before first paint (BOOTSTRAP §8, "Appearance:
 * applying it before first paint"). An external file because the CSP allows no
 * inline script (#99). Plain ES5-style JS, no module syntax: it runs as a
 * classic script ahead of the bundle.
 *
 * Worked from a table of keys, so a later palette picker adds a row and
 * changes none of the mechanism. After boot the React provider
 * (src/lib/appearance.tsx) is the one writer; it calls this same engine.
 */
(function (root) {
  "use strict";

  var KEYS = [
    {
      name: "appearance",
      attribute: "data-appearance",
      // Values that set the attribute; any other value (system) removes it.
      values: ["light", "dark"],
    },
  ];

  function row(name) {
    for (var i = 0; i < KEYS.length; i++) if (KEYS[i].name === name) return KEYS[i];
    return null;
  }

  function cacheKey(name) {
    return "afloat." + name;
  }

  function read(storage, name) {
    try {
      return storage.getItem(cacheKey(name));
    } catch (e) {
      return null;
    }
  }

  function write(storage, name, value) {
    try {
      storage.setItem(cacheKey(name), value);
    } catch (e) {
      // Private window or blocked storage: the cache is only a convenience.
    }
  }

  // index.html carries a light and a dark <meta name="theme-color"> that follow
  // the OS. An explicit choice points both at the chosen scheme's colour; system
  // restores each one's own, remembered in data-system the first time.
  function syncThemeColor(doc, value) {
    var metas = doc.querySelectorAll('meta[name="theme-color"]');
    var own = { light: null, dark: null };
    var i, meta, media;
    for (i = 0; i < metas.length; i++) {
      meta = metas[i];
      if (!meta.getAttribute("data-system")) {
        meta.setAttribute("data-system", meta.getAttribute("content") || "");
      }
      media = meta.getAttribute("media") || "";
      if (media.indexOf("dark") >= 0) own.dark = meta.getAttribute("data-system");
      else if (media.indexOf("light") >= 0) own.light = meta.getAttribute("data-system");
    }
    for (i = 0; i < metas.length; i++) {
      meta = metas[i];
      var colour = value === "light" ? own.light : value === "dark" ? own.dark : null;
      meta.setAttribute("content", colour || meta.getAttribute("data-system") || "");
    }
  }

  function apply(doc, name, value) {
    var r = row(name);
    if (!r) return;
    var el = doc.documentElement;
    if (r.values.indexOf(value) >= 0) el.setAttribute(r.attribute, value);
    else el.removeAttribute(r.attribute);
    if (name === "appearance") syncThemeColor(doc, r.values.indexOf(value) >= 0 ? value : "system");
  }

  function boot(doc, storage) {
    for (var i = 0; i < KEYS.length; i++) {
      apply(doc, KEYS[i].name, read(storage, KEYS[i].name));
    }
  }

  var engine = {
    keys: KEYS,
    cacheKey: cacheKey,
    read: read,
    write: write,
    apply: apply,
    boot: boot,
  };

  // Under test the engine is also handed back through CommonJS exports.
  if (typeof module !== "undefined" && module.exports) module.exports = engine;
  root.afloatAppearance = engine;
  var storage = null;
  try {
    storage = root.localStorage;
  } catch (e) {
    // Access itself can throw; boot with no cache.
  }
  boot(
    root.document,
    storage || {
      getItem: function () {
        return null;
      },
    },
  );
})(typeof window !== "undefined" ? window : this);
