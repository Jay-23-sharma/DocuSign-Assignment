import { createRoot } from "react-dom/client";
import { currentPage } from "./app/router";
import "./styles.css";

const root = createRoot(document.getElementById("root")!);
function renderPage() {
  root.render(currentPage());
}

window.addEventListener("hashchange", renderPage);
renderPage();
