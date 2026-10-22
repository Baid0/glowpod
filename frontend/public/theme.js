// Applies the saved theme before the app loads, so someone who chose light
// doesn't see a dark flash. An external file, because the CSP blocks inline
// scripts. Keep the key in sync with KEYS.theme in src/lib/preferences.ts.
try {
  var choice = window.localStorage.getItem("glowpod.theme");
  if (
    choice === "light" ||
    (choice === "system" && window.matchMedia("(prefers-color-scheme: light)").matches)
  ) {
    document.documentElement.setAttribute("data-theme", "light");
  }
} catch {
  // storage unavailable: dark, the default
}
