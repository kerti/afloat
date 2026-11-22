import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useNavigate } from "react-router";
import { useAuthMethods, useMe, useRegister } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { AuthCard } from "@/components/AuthCard";
import { Field, Form } from "@/components/Form";
import { FieldError, FieldHint, MutedText, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { routes } from "@/lib/routes";
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
  const [userDisplayName, setUserDisplayName] = useState("");

  if (me.data) return <Navigate to={routes.home} replace />;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const name = userDisplayName.trim();
    register.mutate(
      {
        email,
        password,
        ...(name ? { user_display_name: name } : {}),
        // The language the person is reading this in: the server stores it as
        // their locale, and falls back to en-GB for anything it does not know.
        locale: i18n.language,
      },
      { onSuccess: () => void navigate(routes.onboarding) },
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
    <AuthCard>
      <PageTitle>{t("register.title")}</PageTitle>
      {localOff || closed ? (
        <>
          <MutedText role="status">
            {localOff ? t("register.unavailable") : t("register.closed")}
          </MutedText>
          <TextLink to={routes.login}>{t("register.signIn")}</TextLink>
        </>
      ) : (
        <Form onSubmit={onSubmit}>
          <Field>
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
          </Field>
          <Field>
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
            <FieldHint id="password-hint">{t("register.passwordHint")}</FieldHint>
          </Field>
          <Field>
            <Label htmlFor="user-display-name">{t("fields.userDisplayName")}</Label>
            <Input
              id="user-display-name"
              autoComplete="name"
              value={userDisplayName}
              onChange={(e) => setUserDisplayName(e.target.value)}
            />
          </Field>
          {failed && <FieldError>{failed}</FieldError>}
          <Button type="submit" disabled={register.isPending}>
            {register.isPending ? t("register.submitting") : t("register.submit")}
          </Button>
          <FieldHint>
            {t("register.haveAccount")}{" "}
            <TextLink to={routes.login}>{t("register.signIn")}</TextLink>
          </FieldHint>
        </Form>
      )}
    </AuthCard>
  );
}
