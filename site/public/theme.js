// Applies the saved theme before the page paints, so someone who chose light doesn't see a dark
// flash. An external file, because the CSP blocks inline scripts. Dark is the default.
try {
  if (window.localStorage.getItem("glowpod.theme") === "light") {
    document.documentElement.setAttribute("data-theme", "light");
  }
} catch {
  // storage unavailable: dark
}
document.documentElement.classList.add("js");
