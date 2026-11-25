import { useEffect, useState, type FormEvent } from "react";
import { post } from "../../lib/api";

export function AccountForm({ mode }: { mode: "register" | "login" }) {
  const registering = mode === "register";
  const title = registering ? "Create account" : "Log in";
  const [pending, setPending] = useState(false);
  const [accepted, setAccepted] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => { document.title = title + " · LocalSign"; }, [title]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const email = String(data.get("email")).trim();
    const password = String(data.get("password"));
    const name = String(data.get("name") ?? "").trim();
    setError("");

    if (registering) {
      if (!name || Array.from(name).length > 200 || name.includes("\0")) {
        setError("Name must contain 1 to 200 characters.");
        return;
      }
      if (new TextEncoder().encode(email.toLowerCase()).length > 254) {
        setError("Enter a valid email address.");
        return;
      }
      const length = Array.from(password).length;
      if (length < 12 || length > 128) {
        setError("Password must contain 12 to 128 characters.");
        return;
      }
    }

    setPending(true);
    try {
      const response = await post(mode, registering ? { name, email, password } : { email, password });
      if (registering && response.status === 202) {
        form.reset();
        setAccepted(true);
      } else if (!registering && response.status === 200) {
        form.reset();
        window.location.assign("/");
      } else if (!registering && response.status === 401) {
        setError("Invalid credentials.");
      } else if (registering && (response.status === 400 || response.status === 413)) {
        const details = await response.json().catch(() => null);
        setError(typeof details?.error === "string" ? details.error : "Check your details and try again.");
      } else {
        setError("The account service is unavailable. Please try again.");
      }
    } catch {
      setError("Could not connect. Check your connection and try again.");
    } finally {
      setPending(false);
    }
  }

  return <main>
    <h1>{title}</h1>
    {accepted ? <p role="status">If your account needs verification, check your email.</p> :
      <form method="post" onSubmit={submit}>
        {registering && <>
          <label htmlFor="name">Name</label>
          <input id="name" name="name" autoComplete="name" required />
        </>}
        <label htmlFor="email">Email</label>
        <input id="email" name="email" type="email" autoComplete={registering ? "email" : "username"} required />
        <label htmlFor="password">Password</label>
        <input id="password" name="password" type="password" required
          autoComplete={registering ? "new-password" : "current-password"}
          aria-describedby={registering ? "password-limit" : undefined} />
        {registering && <small id="password-limit">12 to 128 characters</small>}
        {error && <p role="alert">{error}</p>}
        <button disabled={pending}>{title}</button>
      </form>}
    <a href={registering ? "/login" : "/register"}>{registering ? "Log in" : "Create account"}</a>
    <a href="/resend-verification">Request a new verification link</a>
  </main>;
}
