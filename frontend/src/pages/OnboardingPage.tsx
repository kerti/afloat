import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useNavigate } from "react-router";
import { useCommitChoice, useMe, useOnboardingOptions } from "@/api/queries";
import type { OnboardingChoiceRequest } from "@/api/client";
import { errorMessage } from "@/api/errors";
import { AuthCard } from "@/components/AuthCard";
import { FieldError, MutedText, PageTitle } from "@/components/Text";
import { TextLink } from "@/components/TextLink";
import { routes } from "@/lib/routes";
import { ErrorState } from "@/components/states";
import { Button } from "@/components/ui/button";
import type { FoundedState } from "@/pages/HomePage";

// Founding is silent (#140 R2): register lands here and this page commits the
// choice itself, with the names the catalogue suggests, then goes to Home. It
// survives as three states only: pending, a failure with Try again (the same
// body is resent), and founding closed. The handshake cookie is the credential,
// so the page holds nothing of its own: a 401 from either call means it is gone
// and the person starts again at sign-in. When a real choice returns (a pending
// invitation) the gate returns with it.
export function OnboardingPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const me = useMe();
  const options = useOnboardingOptions();
  const commit = useCommitChoice();
  // Composed once, when the options arrive, so Try again resends the same body.
  const body = useRef<OnboardingChoiceRequest | null>(null);

  const gate = options.data;
  const open = gate?.founding_open === true;

  function send() {
    const sent = body.current!;
    commit
      .mutateAsync(sent)
      .then(() => {
        const founded: FoundedState = { founded: true, pocket: sent.pocket_display_name };
        void navigate(routes.home, { replace: true, state: founded });
      })
      .catch(() => {});
  }

  // Once: the second commit of a spent handshake would answer 401.
  useEffect(() => {
    if (!gate || !open || body.current) return;
    body.current = {
      found: true,
      household_display_name: t("onboarding.householdNameDefault", {
        name: gate.user_display_name,
      }),
      pocket_display_name: t("onboarding.pocketDefault"),
    };
    send();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [gate, open]);

  // Already signed in: nothing to found. Not once a commit has started, whose
  // own 204 makes /me answer a render before the navigation below.
  if (me.data && commit.isIdle) return <Navigate to={routes.home} replace />;

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

  const closed = (gate && !open) || commit.error?.code === "FOUNDING_DISABLED";

  return (
    <AuthCard>
      <PageTitle>{t("onboarding.title")}</PageTitle>
      {closed ? (
        <>
          <MutedText role="status">{t("onboarding.closed")}</MutedText>
          <TextLink to={routes.login}>{t("onboarding.backToSignIn")}</TextLink>
        </>
      ) : commit.error ? (
        <>
          <FieldError>
            {errorMessage(t, commit.error)}
            {commit.error.code === "EMAIL_TAKEN" && (
              <>
                {" "}
                <TextLink to={routes.login}>{t("register.signIn")}</TextLink>
              </>
            )}
          </FieldError>
          <Button onClick={send}>{t("onboarding.retry")}</Button>
        </>
      ) : (
        <MutedText role="status">{t("onboarding.pending")}</MutedText>
      )}
    </AuthCard>
  );
}
