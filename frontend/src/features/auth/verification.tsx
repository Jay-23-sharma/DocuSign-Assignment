import { useState, type FormEvent } from "react";
import { post } from "../../lib/api";

export function VerificationPage({ token }: { token: string }) {
  const [state, setState] = useState<"ready" | "busy" | "verified" | "invalid" | "error">(token ? "ready" : "invalid");
  async function verify() {
    setState("busy");
    try {
      const response = await post("verify-email", { token });
      setState(response.status === 200 ? "verified" : response.status === 400 ? "invalid" : "error");
    } catch {
      setState("error");
    }
  }
  return <main>
    <h1>Verify your email</h1>
    <div role="status">
      {state === "verified" && "Your email is verified."}
      {state === "invalid" && "This verification link is invalid or has expired."}
      {state === "error" && "Verification is unavailable. Please try again."}
    </div>
    {state !== "verified" && state !== "invalid" &&
      <button disabled={state === "busy"} onClick={verify}>Verify email</button>}
    {state === "verified" && <a href="/login">Continue to login</a>}
    {state !== "verified" && <a href="/resend-verification">Request a new verification link</a>}
  </main>;
}

export function ResendPage() {
  const [email, setEmail] = useState("");
  const [state, setState] = useState<"ready" | "busy" | "accepted" | "error">("ready");
  async function resend(event: FormEvent) {
    event.preventDefault();
    setState("busy");
    try {
      const response = await post("resend-verification", { email });
      setState(response.status === 202 ? "accepted" : "error");
    } catch {
      setState("error");
    }
  }
  return <main>
    <h1>Request a verification link</h1>
    <form onSubmit={resend}>
      <label htmlFor="email">Account email</label>
      <input id="email" type="email" autoComplete="email" value={email} required
        onChange={event => { setEmail(event.target.value); setState("ready"); }} />
      <button disabled={state === "busy"}>Request link</button>
    </form>
    <div role="status">
      {state === "accepted" && "Request accepted. Check your inbox for an available verification link."}
      {state === "error" && "The request could not be completed. Please try again."}
    </div>
    <a href="/login">Log in</a>
  </main>;
}
