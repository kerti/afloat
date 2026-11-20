import { useMemo, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import type { Me, UpdateMeRequest } from "@/api/client";
import { useAuthMethods, useChangeEmail, useChangePassword, useUpdateMe } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

const selectClass =
  "h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-xs aria-invalid:border-destructive";

const locales = [
  { value: "en-GB", key: "enGB" },
  { value: "id-ID", key: "idID" },
] as const;

// The browser's own list, plus the stored value so a name the browser lacks
// still shows. A name the server refuses comes back as VALIDATION.
function timeZoneNames(stored: string): string[] {
  let names: string[] = [];
  try {
    names = Intl.supportedValuesOf("timeZone");
  } catch {
    // Older engines: the stored value alone is still a valid choice.
  }
  return [...new Set([...names, stored])].sort();
}

// Three forms, each with its own Save and its own error, so a failed password
// change never reads as a failed profile save (PRD N14).
export function UserSettingsPage({ me }: { me: Me }) {
  const { t } = useTranslation();
  const methods = useAuthMethods();
  // Shown only on a positive answer, so the sections never flash and vanish.
  const local = methods.data?.local === true;

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-md flex-col gap-8 px-4 py-8">
      <header className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold">{t("settings.user.title")}</h1>
        <Link to="/" className="text-sm underline">
          {t("settings.user.back")}
        </Link>
      </header>
      <ProfileForm me={me} />
      {local && <EmailForm />}
      {local && <PasswordForm />}
    </main>
  );
}

// Each field is null until typed, so a background /me refetch never overwrites
// what is being edited, and only what changed is sent (the PATCH merges).
function ProfileForm({ me }: { me: Me }) {
  const { t } = useTranslation();
  const update = useUpdateMe();
  const [name, setName] = useState<string | null>(null);
  const [locale, setLocale] = useState<string | null>(null);
  const [zone, setZone] = useState<string | null>(null);

  const zones = useMemo(() => timeZoneNames(me.time_zone), [me.time_zone]);

  const body: UpdateMeRequest = {};
  if (name !== null && name !== me.user_display_name) body.user_display_name = name;
  if (locale !== null && locale !== me.locale) body.locale = locale as Me["locale"];
  if (zone !== null && zone !== me.time_zone) body.time_zone = zone;
  const changed = Object.keys(body).length > 0;

  const invalidField = update.error?.code === "VALIDATION" ? update.error.args.field : undefined;
  const invalid = (field: string) => (invalidField === field ? true : undefined);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!changed) return;
    update.mutate(body, {
      onSuccess: () => {
        setName(null);
        setLocale(null);
        setZone(null);
      },
    });
  }

  return (
    <section aria-labelledby="profile-heading">
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <h2 id="profile-heading" className="text-lg font-medium">
          {t("settings.user.profile.heading")}
        </h2>
        <div className="flex flex-col gap-2">
          <Label htmlFor="user-name">{t("settings.user.profile.name")}</Label>
          <Input
            id="user-name"
            autoComplete="name"
            value={name ?? me.user_display_name}
            onChange={(e) => setName(e.target.value)}
            aria-invalid={invalid("user_display_name")}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="user-locale">{t("settings.user.profile.language")}</Label>
          <select
            id="user-locale"
            className={selectClass}
            value={locale ?? me.locale}
            onChange={(e) => setLocale(e.target.value)}
            aria-invalid={invalid("locale")}
          >
            {locales.map((l) => (
              <option key={l.value} value={l.value}>
                {t(`settings.user.languages.${l.key}`)}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="user-time-zone">{t("settings.user.profile.timeZone")}</Label>
          <select
            id="user-time-zone"
            className={selectClass}
            value={zone ?? me.time_zone}
            onChange={(e) => setZone(e.target.value)}
            aria-invalid={invalid("time_zone")}
          >
            {zones.map((z) => (
              <option key={z} value={z}>
                {z}
              </option>
            ))}
          </select>
        </div>
        {update.error && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(t, update.error)}
          </p>
        )}
        {update.isSuccess && !changed && (
          <p role="status" className="text-sm text-muted-foreground">
            {t("settings.user.profile.saved")}
          </p>
        )}
        <Button type="submit" disabled={!changed || update.isPending}>
          {update.isPending ? t("settings.user.profile.saving") : t("settings.user.profile.save")}
        </Button>
      </form>
    </section>
  );
}

function EmailForm() {
  const { t } = useTranslation();
  const change = useChangeEmail();
  const [email, setEmail] = useState("");
  const [current, setCurrent] = useState("");

  const code = change.error?.code;
  const invalidField = code === "VALIDATION" ? change.error?.args.field : undefined;
  const emailInvalid = code === "EMAIL_TAKEN" || invalidField === "email" ? true : undefined;
  const currentInvalid =
    code === "INVALID_CURRENT_PASSWORD" || invalidField === "current_password" ? true : undefined;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    change.mutate(
      { email: email.trim(), current_password: current },
      {
        onSuccess: () => setEmail(""),
        // The current password is cleared after any answer.
        onSettled: () => setCurrent(""),
      },
    );
  }

  return (
    <section aria-labelledby="email-heading">
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <h2 id="email-heading" className="text-lg font-medium">
          {t("settings.user.email.heading")}
        </h2>
        <div className="flex flex-col gap-2">
          <Label htmlFor="new-email">{t("settings.user.email.newEmail")}</Label>
          <Input
            id="new-email"
            type="email"
            autoComplete="email"
            inputMode="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            aria-invalid={emailInvalid}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="email-current-password">{t("settings.user.email.currentPassword")}</Label>
          <Input
            id="email-current-password"
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            aria-invalid={currentInvalid}
          />
        </div>
        {change.error && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(t, change.error)}
          </p>
        )}
        {change.isSuccess && !email && (
          <p role="status" className="text-sm text-muted-foreground">
            {t("settings.user.email.saved", { email: change.data.email })}
          </p>
        )}
        <Button type="submit" disabled={!email || !current || change.isPending}>
          {change.isPending ? t("settings.user.email.submitting") : t("settings.user.email.submit")}
        </Button>
      </form>
    </section>
  );
}

function PasswordForm() {
  const { t } = useTranslation();
  const change = useChangePassword();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [mismatch, setMismatch] = useState(false);

  const code = change.error?.code;
  const invalidField = code === "VALIDATION" ? change.error?.args.field : undefined;
  const currentInvalid =
    code === "INVALID_CURRENT_PASSWORD" || invalidField === "current_password" ? true : undefined;
  const nextInvalid =
    code === "WEAK_PASSWORD" || invalidField === "new_password" ? true : undefined;

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    // The confirm field is client-only: a typo in the new password would lock
    // the person out, and nothing else can reset it yet.
    if (next !== confirm) {
      setMismatch(true);
      return;
    }
    setMismatch(false);
    change.mutate(
      { current_password: current, new_password: next },
      {
        onSuccess: () => {
          setNext("");
          setConfirm("");
        },
        onSettled: () => setCurrent(""),
      },
    );
  }

  return (
    <section aria-labelledby="password-heading">
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <h2 id="password-heading" className="text-lg font-medium">
          {t("settings.user.password.heading")}
        </h2>
        <p id="password-hint" className="text-sm text-muted-foreground">
          {t("settings.user.password.hint")}
        </p>
        <div className="flex flex-col gap-2">
          <Label htmlFor="password-current">{t("settings.user.password.currentPassword")}</Label>
          <Input
            id="password-current"
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            aria-invalid={currentInvalid}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="password-new">{t("settings.user.password.newPassword")}</Label>
          <Input
            id="password-new"
            type="password"
            autoComplete="new-password"
            value={next}
            onChange={(e) => setNext(e.target.value)}
            aria-invalid={nextInvalid}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="password-confirm">{t("settings.user.password.confirm")}</Label>
          <Input
            id="password-confirm"
            type="password"
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => {
              setConfirm(e.target.value);
              setMismatch(false);
            }}
            aria-invalid={mismatch ? true : undefined}
          />
        </div>
        {mismatch && (
          <p role="alert" className="text-sm text-destructive">
            {t("settings.user.password.mismatch")}
          </p>
        )}
        {change.error && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(t, change.error)}
          </p>
        )}
        {change.isSuccess && !next && !confirm && (
          <p role="status" className="text-sm text-muted-foreground">
            {t("settings.user.password.saved")}
          </p>
        )}
        <Button type="submit" disabled={!current || !next || change.isPending}>
          {change.isPending
            ? t("settings.user.password.submitting")
            : t("settings.user.password.submit")}
        </Button>
      </form>
    </section>
  );
}
