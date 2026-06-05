"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { getVolcanoClient } from "../lib/volcano";

export default function HomePage() {
  const router = useRouter();
  const [mode, setMode] = useState("signin");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    async function checkSession() {
      const volcano = getVolcanoClient();
      const session = await volcano.initialize();
      if (session.user) {
        router.push("/dashboard");
      }
    }

    checkSession();
  }, [router]);

  const isSignup = mode === "signup";

  async function handleSubmit(event) {
    event.preventDefault();
    setError("");
    setLoading(true);

    try {
      const volcano = getVolcanoClient();
      const result = isSignup
        ? await volcano.auth.signUp({ email, password })
        : await volcano.auth.signIn({ email, password });

      if (result.error) {
        setError(result.error.message || "Authentication failed");
        setLoading(false);
        return;
      }

      router.push("/dashboard");
    } catch (err) {
      setError(err?.message || "Authentication failed");
      setLoading(false);
      return;
    }

    setLoading(false);
  }

  return (
    <section className="panel auth-panel">
      <p className="eyebrow">Volcano Notes Demo</p>
      <h1>{isSignup ? "Create an account" : "Login"}</h1>
      <p className="lede">
        {isSignup ? "Create your account to start writing private notes." : "Login to continue to your notes dashboard."}
      </p>

      <form onSubmit={handleSubmit} className="stack">
        <label>
          Email
          <input type="email" value={email} onChange={(event) => setEmail(event.target.value)} autoComplete="email" required />
        </label>

        <label>
          Password
          <input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            minLength={6}
            autoComplete={isSignup ? "new-password" : "current-password"}
            required
          />
        </label>

        <button type="submit" className="auth-cta" disabled={loading}>
          {loading ? "Working..." : isSignup ? "Create an account" : "Login"}
        </button>
      </form>

      <p className="switch-copy">
        {isSignup ? "Already have an account?" : "Need an account?"}{" "}
        <button type="button" className="text-button" onClick={() => setMode(isSignup ? "signin" : "signup")} disabled={loading}>
          {isSignup ? "Login" : "Create an account"}
        </button>
      </p>

      {error ? <p className="error">{error}</p> : null}
    </section>
  );
}
