import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./app/App";

const root = document.getElementById("root");
if (!root) {
  throw new Error("Glowpod: #root element is missing from index.html");
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
