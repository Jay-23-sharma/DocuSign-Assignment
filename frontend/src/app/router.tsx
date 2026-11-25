import { Guest } from "../features/signing/Guest";
import { AccountForm } from "../features/auth/accountForm";
import { SenderPage } from "../features/auth/sender";
import { VerificationPage, ResendPage } from "../features/auth/verification";
import { takeToken } from "./verificationToken";

export function currentPage() {
  const preview = /^\/documents\/([1-9]\d*)(\/prepare|\/review)?$/.exec(location.pathname);
  if (preview) return <SenderPage documentId={preview[1]} prepare={preview[2] === "/prepare"} review={preview[2] === "/review"} />;
  switch (location.pathname) {
    case "/sign": return <Guest key={location.hash} />;
    case "/":
      return <SenderPage />;
    case "/login":
      return <AccountForm mode="login" />;
    case "/register":
      return <AccountForm mode="register" />;
    case "/verify-email": {
      const token = takeToken(window);
      return <VerificationPage key={token} token={token} />;
    }
    case "/resend-verification":
      return <ResendPage />;
    default:
      return <main><h1>Page not found</h1><a href="/resend-verification">Request a verification link</a></main>;
  }
}
