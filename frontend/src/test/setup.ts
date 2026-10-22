import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterEach } from "vitest";

// Role queries walk the accessibility tree, which is slow on shared CI
// runners; the 1 s default made findBy* time out there now and then.
configure({ asyncUtilTimeout: 3000 });

// jsdom has no <dialog> behaviour; enough of it for the command palette.
if (typeof HTMLDialogElement !== "undefined" && !("showModal" in HTMLDialogElement.prototype)) {
  Object.assign(HTMLDialogElement.prototype, {
    showModal(this: HTMLDialogElement) {
      this.setAttribute("open", "");
    },
    close(this: HTMLDialogElement) {
      this.removeAttribute("open");
      this.dispatchEvent(new Event("close"));
    },
  });
  Object.defineProperty(HTMLDialogElement.prototype, "open", {
    get(this: HTMLDialogElement) {
      return this.hasAttribute("open");
    },
    configurable: true,
  });
}

// jsdom has no layout, so every element measures 0×0 and virtualized lists
// render nothing. Give elements a desktop-sized box.
Object.defineProperties(HTMLElement.prototype, {
  offsetHeight: { configurable: true, get: () => 800 },
  offsetWidth: { configurable: true, get: () => 1200 },
});
Object.defineProperties(Element.prototype, {
  clientHeight: { configurable: true, get: () => 800 },
  clientWidth: { configurable: true, get: () => 1200 },
});

// jsdom doesn't scroll.
Element.prototype.scrollIntoView = function scrollIntoView() {
  // nothing to do without layout
};

afterEach(() => {
  cleanup();
  try {
    window.localStorage.clear();
  } catch {
    // storage unavailable
  }
});
