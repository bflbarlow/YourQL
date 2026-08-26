// YourQL — theme / accent / scale engine (moved verbatim in behavior from main.js).
// Single owner of data-theme, data-ui-scale and accent CSS variables.
// SettingsView imports this module directly; no window.* globals.

import { UpdateGeneralSettings } from "../../wailsjs/go/main/App.js";

const THEME_KEY = "yourql-theme"; // 'light' | 'dark' | 'system'
const ACCENT_KEY = "yourql-accent";
const SCALE_KEY = "yourql-ui-scale";
const DEFAULT_THEME_SELECTION = "system";
const DEFAULT_ACCENT = "#0288d1";

// ==================== HSL helpers ====================
function hexToHSL(hex) {
  let r = 0,
    g = 0,
    b = 0;
  hex = hex.replace("#", "");
  if (hex.length === 3) {
    r = parseInt(hex[0] + hex[0], 16) / 255;
    g = parseInt(hex[1] + hex[1], 16) / 255;
    b = parseInt(hex[2] + hex[2], 16) / 255;
  } else {
    r = parseInt(hex.substring(0, 2), 16) / 255;
    g = parseInt(hex.substring(2, 4), 16) / 255;
    b = parseInt(hex.substring(4, 6), 16) / 255;
  }
  const max = Math.max(r, g, b),
    min = Math.min(r, g, b);
  let h = 0,
    s = 0;
  const l = (max + min) / 2;
  if (max !== min) {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    switch (max) {
      case r:
        h = ((g - b) / d + (g < b ? 6 : 0)) / 6;
        break;
      case g:
        h = ((b - r) / d + 2) / 6;
        break;
      case b:
        h = ((r - g) / d + 4) / 6;
        break;
    }
  }
  return { h, s, l };
}

function hslToHex(h, s, l) {
  const hue2rgb = (p, q, t) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  };
  if (s === 0) {
    const v = Math.round(l * 255);
    return "#" + [v, v, v].map((c) => c.toString(16).padStart(2, "0")).join("");
  }
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const r = Math.round(hue2rgb(p, q, h + 1 / 3) * 255);
  const g = Math.round(hue2rgb(p, q, h) * 255);
  const b = Math.round(hue2rgb(p, q, h - 1 / 3) * 255);
  return "#" + [r, g, b].map((c) => c.toString(16).padStart(2, "0")).join("");
}

export function darken(hex, pct) {
  const { h, s, l } = hexToHSL(hex);
  return hslToHex(h, s, Math.max(0, l - pct / 100));
}

export function lighten(hex, pct) {
  const { h, s, l } = hexToHSL(hex);
  return hslToHex(h, s, Math.min(1, l + pct / 100));
}

function hexToRgba(hex, alpha) {
  hex = hex.replace("#", "");
  if (hex.length === 3)
    hex = hex
      .split("")
      .map((c) => c + c)
      .join("");
  const r = parseInt(hex.substring(0, 2), 16);
  const g = parseInt(hex.substring(2, 4), 16);
  const b = parseInt(hex.substring(4, 6), 16);
  return `rgba(${r},${g},${b},${alpha})`;
}

// ==================== Scale ====================
export function applyScale(scale) {
  document.documentElement.setAttribute("data-ui-scale", scale);
  localStorage.setItem(SCALE_KEY, scale);
  UpdateGeneralSettings({ scale }).catch(() => {});
}

export function getScale() {
  return localStorage.getItem(SCALE_KEY) || "medium";
}

// ==================== Accent ====================
export function applyAccent(hex) {
  if (!/^#?([a-f\d]{3}){1,2}$/i.test(hex)) return;
  if (hex.length === 4)
    hex =
      "#" +
      hex
        .slice(1)
        .split("")
        .map((c) => c + c)
        .join("");
  const root = document.documentElement;
  root.style.setProperty("--color-accent", hex);
  root.style.setProperty("--color-accent-hover", darken(hex, 10));
  root.style.setProperty("--color-accent-light", hexToRgba(hex, 0.1));
  root.style.setProperty("--color-accent-border", hexToRgba(hex, 0.3));
  root.style.setProperty("--color-accent-print", hex);
  localStorage.setItem(ACCENT_KEY, hex);
}

export function getAccent() {
  return localStorage.getItem(ACCENT_KEY) || DEFAULT_ACCENT;
}

// ==================== Theme ====================
export function resolveTheme(selection) {
  if (selection === "system") {
    if (typeof window !== "undefined" && window.matchMedia) {
      return window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light";
    }
    return "light";
  }
  return selection;
}

const listeners = new Set();
export function onThemeChange(cb) {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

function notifyTheme(resolved) {
  for (const cb of listeners) {
    try {
      cb(resolved);
    } catch (e) {
      console.error(e);
    }
  }
  if (typeof window !== "undefined") {
    window.dispatchEvent(
      new CustomEvent("theme-change", { detail: { theme: resolved } }),
    );
  }
}

export function applyResolvedTheme(resolved) {
  document.documentElement.setAttribute("data-theme", resolved);
  // Re-apply accent so the dark-mode variant is correct.
  const currentAccent = getAccent();
  if (resolved === "dark") {
    const lightened = lighten(currentAccent, 30);
    document.documentElement.style.setProperty("--color-accent", lightened);
    document.documentElement.style.setProperty(
      "--color-accent-hover",
      lighten(lightened, 10),
    );
    document.documentElement.style.setProperty(
      "--color-accent-light",
      hexToRgba(lightened, 0.15),
    );
    document.documentElement.style.setProperty(
      "--color-accent-border",
      hexToRgba(lightened, 0.4),
    );
  } else {
    applyAccent(currentAccent);
  }
  notifyTheme(resolved);
}

export function setThemeSelection(selection) {
  localStorage.setItem(THEME_KEY, selection);
  applyResolvedTheme(resolveTheme(selection));
  UpdateGeneralSettings({ theme: selection }).catch(() => {});
}

export function setAccent(hex) {
  applyAccent(hex);
  const selection = localStorage.getItem(THEME_KEY) || DEFAULT_THEME_SELECTION;
  applyResolvedTheme(resolveTheme(selection));
  UpdateGeneralSettings({ accent: hex }).catch(() => {});
}

export function getThemeSelection() {
  return localStorage.getItem(THEME_KEY) || DEFAULT_THEME_SELECTION;
}

// System preference listener — only meaningful when user chose "system"
if (typeof window !== "undefined" && window.matchMedia) {
  window
    .matchMedia("(prefers-color-scheme: dark)")
    .addEventListener("change", () => {
      if (getThemeSelection() === "system") {
        applyResolvedTheme(resolveTheme("system"));
      }
    });
}

// ==================== Init & DB sync ====================
export async function initFromStorage(GetGeneralSettings) {
  const savedSelection =
    localStorage.getItem(THEME_KEY) || DEFAULT_THEME_SELECTION;
  const savedAccent = localStorage.getItem(ACCENT_KEY) || DEFAULT_ACCENT;
  applyAccent(savedAccent);
  applyResolvedTheme(resolveTheme(savedSelection));

  try {
    const settings = await GetGeneralSettings();
    const localTheme = localStorage.getItem(THEME_KEY);
    const localAccent = localStorage.getItem(ACCENT_KEY);
    const localScale = localStorage.getItem(SCALE_KEY);

    const dbTheme = settings.theme;
    const dbAccent = settings.accent;
    const dbScale = settings.scale;

    const hasDBTheme = dbTheme && dbTheme !== "system";
    const hasDBAccent = dbAccent && dbAccent !== DEFAULT_ACCENT;
    const hasDBScale = dbScale && dbScale !== "medium";

    if (localTheme && !hasDBTheme && localTheme !== "system") {
      UpdateGeneralSettings({ theme: localTheme }).catch(() => {});
    }
    if (localAccent && !hasDBAccent && localAccent !== DEFAULT_ACCENT) {
      UpdateGeneralSettings({ accent: localAccent }).catch(() => {});
    }
    if (localScale && !hasDBScale && localScale !== "medium") {
      UpdateGeneralSettings({ scale: localScale }).catch(() => {});
    }

    const finalTheme = hasDBTheme ? dbTheme : localTheme || "system";
    const finalAccent = hasDBAccent ? dbAccent : localAccent || DEFAULT_ACCENT;
    const finalScale = hasDBScale ? dbScale : localScale || "medium";

    if (finalTheme !== localTheme) setThemeSelection(finalTheme);
    if (finalAccent !== localAccent) {
      applyAccent(finalAccent);
      applyResolvedTheme(resolveTheme(finalTheme));
    }
    if (finalScale !== localScale) applyScale(finalScale);

    localStorage.setItem(THEME_KEY, finalTheme);
    localStorage.setItem(ACCENT_KEY, finalAccent);
    localStorage.setItem(SCALE_KEY, finalScale);
  } catch {
    // DB not ready or empty — localStorage values already applied above.
  }
}
