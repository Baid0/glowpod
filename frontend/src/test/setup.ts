import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

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

afterEach(() => {
  cleanup();
  try {
    window.localStorage.clear();
  } catch {
    // storage unavailable
  }
});
