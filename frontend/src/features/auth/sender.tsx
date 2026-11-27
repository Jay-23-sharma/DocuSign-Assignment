import { useEffect, useState } from "react";
import { getSender, post } from "../../lib/api";

import { Dashboard } from "../documents/Dashboard";
import { Preview } from "../documents/Preview";

type Identity = { state: "loading" | "signed-out" | "error" } | { state: "ready"; name: string };

export function SenderPage({ documentId, prepare, review }: { documentId?: string; prepare?: boolean; review?: boolean }) {
  const [identity, setIdentity] = useState<Identity>({ state: "loading" });
  const [attempt, setAttempt] = useState(0);

  const [logout, setLogout] = useState<"idle" | "pending" | "failed">("idle");

  const [dirty, setDirty] = useState(false);

  async function logOut() {
    if (dirty && !window.confirm("Discard unsaved layout edits and log out?")) return;
    setLogout("pending");
    try {
      const response = await post("logout", {});
      if (response.status !== 204) throw new Error("Logout unconfirmed");
      setIdentity({ state: "signed-out" });
      setLogout("idle");
      const channel = new BroadcastChannel("localsign-session");
      channel.postMessage("logout");
      channel.close();
    } catch {
      setLogout("failed");
    }
  }

  useEffect(() => {
    document.title = "Sender account · LocalSign";
    const controller = new AbortController();
    async function load() {
      try {
        const response = await getSender(controller.signal);
        let result: Identity;
        if (response.status === 401) {
          result = { state: "signed-out" };
        } else if (response.status === 200) {
          const sender = await response.json();
          if (typeof sender?.name !== "string") throw new Error("Invalid sender response");
          result = { state: "ready", name: sender.name };
        } else {
          result = { state: "error" };
        }
        if (!controller.signal.aborted) setIdentity(result);
      } catch {
        if (!controller.signal.aborted) setIdentity({ state: "error" });
      }
    }
    const channel = new BroadcastChannel("localsign-session");
    channel.onmessage = event => {
      if (event.data === "logout") {
        controller.abort();
        setIdentity({ state: "signed-out" });
      }
    };
    void load();
    function restored(event: PageTransitionEvent) {
      if (event.persisted) window.location.reload();
    }

    window.addEventListener("pageshow", restored);
    return () => {
      controller.abort();
      channel.close();
      window.removeEventListener("pageshow", restored);
    };
  }, [attempt]);

  function retry() {
    setIdentity({ state: "loading" });
    setAttempt(value => value + 1);
  }

  const logoutControl = <>
    {logout === "failed" && <p role="alert">Logout could not be confirmed. Please try again.</p>}
    <button className="logout-button" disabled={logout === "pending"} onClick={logOut}>
      {logout === "pending" ? "Logging out…" : logout === "failed" ? "Retry logout" : "Log out"}
    </button>
  </>;
  if (identity.state === "ready" && !documentId) {
    return <Dashboard name={identity.name} logoutControl={logoutControl} />;
  }

  return <main className={identity.state === "ready" ? "workspace" : undefined}>
    <h1>{identity.state === "ready" ? "Documents" : "Sender account"}</h1>
    <div role="status">
      {identity.state === "loading" && "Loading your account…"}
      {identity.state === "ready" && <>Signed in as <strong>{identity.name}</strong>.</>}
      {identity.state === "signed-out" && "You are signed out."}
      {identity.state === "error" && "Your account could not be loaded."}
    </div>
    {identity.state === "signed-out" && <nav aria-label="Account">
      <a href="/login">Log in</a>
      <a href="/register">Create account</a>
    </nav>}
    {identity.state === "ready" && <>
      {logoutControl}
      {documentId && <Preview key={documentId} id={documentId} prepare={prepare} review={review} onDirty={setDirty} />}
    </>}
    {identity.state === "error" && <button onClick={retry}>Retry</button>}
  </main>;
}
