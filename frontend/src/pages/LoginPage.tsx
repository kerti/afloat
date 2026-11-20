import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link, Navigate } from "react-router";
import { useAuthMethods, useLogin, useMe } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function LoginPage() {
  const { t } = useTranslation();
  const me = useMe();
  const methods = useAuthMethods();
  const login = useLogin();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  if (me.data) return <Navigate to="/" replace />;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    login.mutate({ email, password });
  }

  const localOff = methods.data?.local === false;
  // Display only: the gate's commit is what enforces founding_open.
  const canRegister = methods.data?.local === true && methods.data.founding_open;
  const failed = login.error ? errorMessage(t, login.error) : null;

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-sm flex-col justify-center gap-6 px-4">
      <h1 className="text-2xl font-semibold">{t("login.title")}</h1>
      {localOff ? (
        <p role="status" className="text-muted-foreground">
          {t("login.unavailable")}
        </p>
      ) : (
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          <div className="flex flex-col gap-2">
            <Label htmlFor="email">{t("fields.email")}</Label>
            <Input
              id="email"
              type="email"
              autoComplete="username"
              inputMode="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              aria-invalid={login.error?.code === "INVALID_CREDENTIALS" || undefined}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="password">{t("fields.password")}</Label>
            <Input
              id="password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              aria-invalid={login.error?.code === "INVALID_CREDENTIALS" || undefined}
            />
          </div>
          {failed && (
            <p role="alert" className="text-sm text-destructive">
              {failed}
            </p>
          )}
          <Button type="submit" disabled={login.isPending}>
            {login.isPending ? t("login.submitting") : t("login.submit")}
          </Button>
          {canRegister && (
            <Link to="/register" className="text-sm underline">
              {t("login.createAccount")}
            </Link>
          )}
        </form>
      )}
    </main>
  );
}
