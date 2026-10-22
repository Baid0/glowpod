import "@fontsource-variable/inter";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/components.css";
import "./styles/home.css";
import "./styles/docs.css";

// Keep the key in sync with public/theme.js, which applies it before the page paints.
const THEME_KEY = "glowpod.theme";

function setUpThemeToggle() {
  const root = document.documentElement;
  for (const button of document.querySelectorAll(".theme-toggle")) {
    const label = () =>
      button.setAttribute(
        "aria-label",
        root.dataset.theme === "light" ? "Switch to dark theme" : "Switch to light theme",
      );
    label();
    button.addEventListener("click", () => {
      const light = root.dataset.theme !== "light";
      if (light) root.dataset.theme = "light";
      else delete root.dataset.theme;
      try {
        window.localStorage.setItem(THEME_KEY, light ? "light" : "dark");
      } catch {
        // storage unavailable: the choice lasts for this page only
      }
      label();
    });
  }
}

// A button with aria-controls and aria-expanded shows and hides its target (the site menu, the
// docs menu). Escape closes it and returns focus to the button.
function setUpDisclosures() {
  for (const button of document.querySelectorAll("button[aria-controls][aria-expanded]")) {
    const target = document.getElementById(button.getAttribute("aria-controls"));
    if (!target) continue;
    const set = (open) => {
      button.setAttribute("aria-expanded", String(open));
      target.classList.toggle("is-open", open);
    };
    button.addEventListener("click", () => set(button.getAttribute("aria-expanded") !== "true"));
    const closeOnEscape = (event) => {
      if (event.key === "Escape" && button.getAttribute("aria-expanded") === "true") {
        set(false);
        button.focus();
      }
    };
    button.addEventListener("keydown", closeOnEscape);
    target.addEventListener("keydown", closeOnEscape);
    // Following an in-page link closes the menu.
    target.addEventListener("click", (event) => {
      if (event.target.closest("a")) set(false);
    });
  }
}

function setUpCopyButtons() {
  const status = document.createElement("span");
  status.className = "visually-hidden";
  status.setAttribute("role", "status");
  document.body.append(status);

  for (const button of document.querySelectorAll("[data-copy]")) {
    let timer;
    button.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(button.dataset.copy);
        status.textContent = "Copied to the clipboard";
        button.classList.add("is-copied");
        clearTimeout(timer);
        timer = setTimeout(() => {
          button.classList.remove("is-copied");
          status.textContent = "";
        }, 2000);
      } catch {
        status.textContent = "Couldn't copy. Select the text and copy it yourself.";
      }
    });
  }
}

// Tabs with the ARIA pattern: arrow keys, Home and End move between tabs and select them.
function setUpTabs() {
  for (const list of document.querySelectorAll('[role="tablist"]')) {
    const tabs = [...list.querySelectorAll('[role="tab"]')];
    const select = (tab, focus) => {
      for (const other of tabs) {
        const selected = other === tab;
        other.setAttribute("aria-selected", String(selected));
        other.tabIndex = selected ? 0 : -1;
        document.getElementById(other.getAttribute("aria-controls")).hidden = !selected;
      }
      if (focus) tab.focus();
    };
    select(tabs.find((tab) => tab.getAttribute("aria-selected") === "true") ?? tabs[0], false);
    list.addEventListener("click", (event) => {
      const tab = event.target.closest('[role="tab"]');
      if (tab) select(tab, false);
    });
    list.addEventListener("keydown", (event) => {
      const index = tabs.indexOf(document.activeElement);
      if (index < 0) return;
      const next = {
        ArrowRight: tabs[(index + 1) % tabs.length],
        ArrowLeft: tabs[(index - 1 + tabs.length) % tabs.length],
        Home: tabs[0],
        End: tabs[tabs.length - 1],
      }[event.key];
      if (next) {
        event.preventDefault();
        select(next, true);
      }
    });
  }
}

// Marks the "On this page" link for the section being read.
function setUpTableOfContents() {
  const links = [...document.querySelectorAll(".toc a[href^='#']")];
  const sections = links.map((link) => document.getElementById(link.hash.slice(1)));
  if (!links.length || sections.includes(null)) return;
  const update = () => {
    let current = 0;
    sections.forEach((section, i) => {
      if (section.getBoundingClientRect().top < window.innerHeight * 0.3) current = i;
    });
    links.forEach((link, i) => link.classList.toggle("is-current", i === current));
  };
  update();
  window.addEventListener("scroll", update, { passive: true });
}

setUpThemeToggle();
setUpDisclosures();
setUpCopyButtons();
setUpTabs();
setUpTableOfContents();
