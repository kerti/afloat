import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link, Navigate } from "react-router";
import { useCommitChoice, useMe, useOnboardingOptions } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

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
  if (me.data) return <Navigate to="/" replace />;

  if (options.isPending) {
    return (
      <p role="status" className="p-4 text-muted-foreground">
        {t("app.loading")}
      </p>
    );
  }
  // Not while a commit is settling: its own 204 spends the handshake, and a
  // refetch of the options would then answer 401 for the person who just won.
  if ((options.error?.status === 401 || commit.error?.status === 401) && !commit.isSuccess) {
    return <Navigate to="/login" replace />;
  }
  if (options.error) {
    return (
      <div className="flex flex-col items-start gap-3 p-4">
        <p role="alert" className="text-destructive">
          {errorMessage(t, options.error)}
        </p>
        <Button variant="outline" size="sm" onClick={() => void options.refetch()}>
          {t("app.retry")}
        </Button>
      </div>
    );
  }

  const gate = options.data;
  const suggestion = t("onboarding.householdNameDefault", { name: gate.display_name });
  const householdName = edited ?? suggestion;
  const closed = !gate.founding_open || commit.error?.code === "FOUNDING_DISABLED";

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    commit.mutate({ found: true, household_name: householdName });
  }

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-sm flex-col justify-center gap-6 px-4">
      <h1 className="text-2xl font-semibold">{t("onboarding.title")}</h1>
      {closed ? (
        <>
          <p role="status" className="text-muted-foreground">
            {t("onboarding.closed")}
          </p>
          <Link to="/login" className="text-sm underline">
            {t("onboarding.backToSignIn")}
          </Link>
        </>
      ) : (
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          <p className="text-muted-foreground">{t("onboarding.intro", { email: gate.email })}</p>
          <div className="flex flex-col gap-2">
            <Label htmlFor="household-name">{t("fields.householdName")}</Label>
            <Input
              id="household-name"
              required
              value={householdName}
              onChange={(e) => setEdited(e.target.value)}
              aria-invalid={commit.error?.code === "VALIDATION" || undefined}
            />
          </div>
          {commit.error && (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage(t, commit.error)}
              {commit.error.code === "EMAIL_TAKEN" && (
                <>
                  {" "}
                  <Link to="/login" className="underline">
                    {t("register.signIn")}
                  </Link>
                </>
              )}
            </p>
          )}
          <Button type="submit" disabled={commit.isPending}>
            {commit.isPending ? t("onboarding.submitting") : t("onboarding.submit")}
          </Button>
        </form>
      )}
    </main>
  );
}
