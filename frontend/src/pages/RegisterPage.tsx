import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link, Navigate, useNavigate } from "react-router";
import { useAuthMethods, useMe, useRegister } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// Registering creates no account: it opens a short-lived handshake and the
// account is born when the person commits at the gate (/onboarding). So the
// answer to a 204 here is to go there, never to /me.
export function RegisterPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const me = useMe();
  const methods = useAuthMethods();
  const register = useRegister();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");

  if (me.data) return <Navigate to="/" replace />;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const name = displayName.trim();
    register.mutate(
      {
        email,
        password,
        ...(name ? { display_name: name } : {}),
        // The language the person is reading this in: the server stores it as
        // their locale, and falls back to en-GB for anything it does not know.
        locale: i18n.language,
      },
      { onSuccess: () => void navigate("/onboarding") },
    );
  }

  // The methods answer decides what is offered; the gate's commit is what
  // enforces it (contract: founding_open is display only).
  const localOff = methods.data?.local === false;
  const closed = !localOff && methods.data?.founding_open === false;
  const failed = register.error ? errorMessage(t, register.error) : null;
  const invalid = (...codes: string[]) =>
    (register.error && codes.includes(register.error.code)) || undefined;

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-sm flex-col justify-center gap-6 px-4">
      <h1 className="text-2xl font-semibold">{t("register.title")}</h1>
      {localOff || closed ? (
        <>
          <p role="status" className="text-muted-foreground">
            {localOff ? t("register.unavailable") : t("register.closed")}
          </p>
          <Link to="/login" className="text-sm underline">
            {t("register.signIn")}
          </Link>
        </>
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
              aria-invalid={invalid("EMAIL_TAKEN")}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="password">{t("fields.password")}</Label>
            <Input
              id="password"
              type="password"
              autoComplete="new-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              aria-invalid={invalid("WEAK_PASSWORD")}
              aria-describedby="password-hint"
            />
            <p id="password-hint" className="text-sm text-muted-foreground">
              {t("register.passwordHint")}
            </p>
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="display-name">{t("fields.displayName")}</Label>
            <Input
              id="display-name"
              autoComplete="name"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </div>
          {failed && (
            <p role="alert" className="text-sm text-destructive">
              {failed}
            </p>
          )}
          <Button type="submit" disabled={register.isPending}>
            {register.isPending ? t("register.submitting") : t("register.submit")}
          </Button>
          <p className="text-sm text-muted-foreground">
            {t("register.haveAccount")}{" "}
            <Link to="/login" className="underline">
              {t("register.signIn")}
            </Link>
          </p>
        </form>
      )}
    </main>
  );
}
