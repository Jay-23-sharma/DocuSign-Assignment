import { useState } from "react";
import { createRoot } from "react-dom/client";
import { currentPage } from "./app/router";
import "./styles.css";

type Theme = "light" | "dark";
const preferenceKey = "localsign-theme";

function savedTheme(): Theme {
  try { return localStorage.getItem(preferenceKey) === "dark" ? "dark" : "light"; }
  catch { return "light"; }
}

document.documentElement.dataset.theme = savedTheme();

function SiteHeader() {
  const [theme, setTheme] = useState(document.documentElement.dataset.theme);
  function changeTheme(next: Theme) {
    document.documentElement.dataset.theme = next;
    setTheme(next);
    try { localStorage.setItem(preferenceKey, next); } catch { /* Use this page's choice when storage is unavailable. */ }
  }
  return <header className="site-header">
    <span className="wordmark">Local<span>Sign</span></span>
    <div className="theme-picker" role="group" aria-label="Color theme">
      {(["light", "dark"] as const).map(mode => <button key={mode}
        aria-pressed={theme === mode} onClick={() => changeTheme(mode)}>
        {mode === "light" ? "Light" : "Dark"}
      </button>)}
    </div>
  </header>;
}

const root = createRoot(document.getElementById("root")!);
function renderPage() {
  root.render(<><SiteHeader />{currentPage()}</>);
}

window.addEventListener("hashchange", renderPage);
renderPage();
