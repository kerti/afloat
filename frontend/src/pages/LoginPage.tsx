import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Navigate } from "react-router";
import { useAuthMethods, useLogin, useMe } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { AuthCard } from "@/components/AuthCard";
import { Field, Form } from "@/components/Form";
import { FieldError, MutedText, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { routes } from "@/lib/routes";
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

  if (me.data) return <Navigate to={routes.home} replace />;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    login.mutate({ email, password });
  }

  const localOff = methods.data?.local === false;
  // Display only: the gate's commit is what enforces founding_open.
  const canRegister = methods.data?.local === true && methods.data.founding_open;
  const failed = login.error ? errorMessage(t, login.error) : null;

  return (
    <AuthCard>
      <PageTitle>{t("login.title")}</PageTitle>
      {localOff ? (
        <MutedText role="status">{t("login.unavailable")}</MutedText>
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
              aria-invalid={login.error?.code === "INVALID_CREDENTIALS" || undefined}
            />
          </Field>
          <Field>
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
          </Field>
          {failed && <FieldError>{failed}</FieldError>}
          <Button type="submit" disabled={login.isPending}>
            {login.isPending ? t("login.submitting") : t("login.submit")}
          </Button>
          {canRegister && <TextLink to={routes.register}>{t("login.createAccount")}</TextLink>}
        </Form>
      )}
    </AuthCard>
  );
}
