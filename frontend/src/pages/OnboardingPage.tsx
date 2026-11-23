import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Navigate } from "react-router";
import { useCommitChoice, useMe, useOnboardingOptions } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { AuthCard } from "@/components/AuthCard";
import { Field, Form } from "@/components/Form";
import { FieldError, MutedText, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { routes } from "@/lib/routes";
import { ErrorState, Loading } from "@/components/states";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { clampName } from "@/lib/codePoints";

// The gate between a registered credential and an account. Today it offers one
// choice, founding a Household; invitations add theirs here later. The
// handshake cookie is the credential, so the page holds nothing of its own: a
// 401 from either call means it is gone, and the person starts again at sign-in.
export function OnboardingPage() {
  const { t } = useTranslation();
  const me = useMe();
  const options = useOnboardingOptions();
  const commit = useCommitChoice();
  // null until the person types: the suggestion is then derived from the
  // current language rather than frozen at first render.
  const [edited, setEdited] = useState<string | null>(null);

  // A commit that succeeded makes /me answer; the shell is next.
  if (me.data) return <Navigate to={routes.home} replace />;

  if (options.isPending) {
    return (
      <AuthCard>
        <Loading />
      </AuthCard>
    );
  }
  // Not while a commit is settling: its own 204 spends the handshake, and a
  // refetch of the options would then answer 401 for the person who just won.
  if ((options.error?.status === 401 || commit.error?.status === 401) && !commit.isSuccess) {
    return <Navigate to={routes.login} replace />;
  }
  if (options.error) {
    return (
      <AuthCard>
        <ErrorState error={options.error} onRetry={() => void options.refetch()} />
      </AuthCard>
    );
  }

  const gate = options.data;
  const suggestion = t("onboarding.householdNameDefault", { name: gate.user_display_name });
  const householdName = edited ?? suggestion;
  const closed = !gate.founding_open || commit.error?.code === "FOUNDING_DISABLED";

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    commit.mutate({ found: true, household_display_name: householdName });
  }

  return (
    <AuthCard>
      <PageTitle>{t("onboarding.title")}</PageTitle>
      {closed ? (
        <>
          <MutedText role="status">{t("onboarding.closed")}</MutedText>
          <TextLink to={routes.login}>{t("onboarding.backToSignIn")}</TextLink>
        </>
      ) : (
        <Form onSubmit={onSubmit}>
          <MutedText>{t("onboarding.intro", { email: gate.email })}</MutedText>
          <Field>
            <Label htmlFor="household-name">{t("fields.householdDisplayName")}</Label>
            <Input
              id="household-name"
              required
              value={householdName}
              onChange={(e) => setEdited(clampName(e.target.value))}
              aria-invalid={commit.error?.code === "VALIDATION" || undefined}
            />
          </Field>
          {commit.error && (
            <FieldError>
              {errorMessage(t, commit.error)}
              {commit.error.code === "EMAIL_TAKEN" && (
                <>
                  {" "}
                  <TextLink to={routes.login}>{t("register.signIn")}</TextLink>
                </>
              )}
            </FieldError>
          )}
          <Button type="submit" disabled={commit.isPending}>
            {commit.isPending ? t("onboarding.submitting") : t("onboarding.submit")}
          </Button>
        </Form>
      )}
    </AuthCard>
  );
}
