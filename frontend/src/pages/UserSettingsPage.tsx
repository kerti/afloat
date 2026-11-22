import { useMemo, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { Me } from "@/api/client";
import { useAuthMethods, useChangeEmail, useChangePassword, useUpdateMe } from "@/api/queries";
import { errorMessage } from "@/api/errors";
import { BackLink } from "@/components/BackLink";
import { Field, Form, Section } from "@/components/Form";
import { FieldError, PageTitle } from "@/components/Text";
import { TextSetting } from "@/components/TextSetting";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  NativeSelect,
} from "@/components/ui/select";
import { toastPlain } from "@/lib/plainToast";
import { routes } from "@/lib/routes";
import { useChoice } from "@/lib/useChoice";

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

// Profile fields save by control kind (the name types and has its own Save; the
// language and time zone are choices that save on change). Email and password
// are typed forms with an explicit submit, each with its own error, so a failed
// password change never reads as a failed profile save (PRD N14).
export function UserSettingsPage({ me }: { me: Me }) {
  const { t } = useTranslation();
  const methods = useAuthMethods();
  // Shown only on a positive answer, so the sections never flash and vanish.
  const local = methods.data?.local === true;

  return (
    <>
      <BackLink to={routes.home} label={t("home.title")} />
      <PageTitle>{t("settings.user.title")}</PageTitle>
      <ProfileSection me={me} />
      {local && <EmailForm />}
      {local && <PasswordForm />}
    </>
  );
}

function ProfileSection({ me }: { me: Me }) {
  const { t, i18n } = useTranslation();
  const rename = useUpdateMe();
  const patch = useUpdateMe();
  const zones = useMemo(() => timeZoneNames(me.time_zone), [me.time_zone]);
  // The language flips here, before the "Saved." toast, so the toast is worded
  // in the language just chosen rather than the one it replaced.
  const [locale, chooseLocale] = useChoice<string>(me.locale, (v) =>
    patch
      .mutateAsync({ locale: v as Me["locale"] })
      .then((saved) => i18n.changeLanguage(saved.locale)),
  );
  const [zone, chooseZone] = useChoice(me.time_zone, (v) => patch.mutateAsync({ time_zone: v }));

  return (
    <Section id="profile" title={t("settings.user.profile.heading")}>
      <TextSetting
        id="user-name"
        label={t("settings.user.profile.name")}
        stored={me.user_display_name}
        autoComplete="name"
        save={(name) => rename.mutateAsync({ user_display_name: name })}
        pending={rename.isPending}
        error={rename.error}
        clearError={rename.reset}
      />
      <Field>
        <Label htmlFor="user-locale">{t("settings.user.profile.language")}</Label>
        <Select value={locale} onValueChange={chooseLocale}>
          <SelectTrigger id="user-locale" />
          <SelectContent>
            {locales.map((l) => (
              <SelectItem key={l.value} value={l.value}>
                {t(`settings.user.languages.${l.key}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <Field>
        <Label htmlFor="user-time-zone">{t("settings.user.profile.timeZone")}</Label>
        <NativeSelect id="user-time-zone" value={zone} onChange={(e) => chooseZone(e.target.value)}>
          {zones.map((z) => (
            <option key={z} value={z}>
              {z}
            </option>
          ))}
        </NativeSelect>
      </Field>
    </Section>
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
    // A failure is shown from change.error, so it is swallowed here. The
    // current password is cleared after any answer.
    change
      .mutateAsync({ email: email.trim(), current_password: current })
      .then((me) => {
        setEmail("");
        toastPlain(t("settings.user.email.saved", { email: me.email }));
      })
      .catch(() => {})
      .finally(() => setCurrent(""));
  }

  return (
    <Section id="email" title={t("settings.user.email.heading")}>
      <Form onSubmit={onSubmit}>
        <Field>
          <Label htmlFor="new-email">{t("settings.user.email.newEmail")}</Label>
          <Input
            id="new-email"
            type="email"
            autoComplete="email"
            inputMode="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            disabled={change.isPending}
            aria-invalid={emailInvalid}
          />
        </Field>
        <Field>
          <Label htmlFor="email-current-password">{t("settings.user.email.currentPassword")}</Label>
          <Input
            id="email-current-password"
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            disabled={change.isPending}
            aria-invalid={currentInvalid}
          />
        </Field>
        {change.error && <FieldError>{errorMessage(t, change.error)}</FieldError>}
        <Button type="submit" disabled={!email || !current || change.isPending}>
          {change.isPending ? t("settings.user.email.submitting") : t("settings.user.email.submit")}
        </Button>
      </Form>
    </Section>
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
    change
      .mutateAsync({ current_password: current, new_password: next })
      .then(() => {
        setNext("");
        setConfirm("");
        toastPlain(t("settings.user.password.saved"));
      })
      .catch(() => {})
      .finally(() => setCurrent(""));
  }

  return (
    <Section
      id="password"
      title={t("settings.user.password.heading")}
      description={t("settings.user.password.hint")}
    >
      <Form onSubmit={onSubmit}>
        <Field>
          <Label htmlFor="password-current">{t("settings.user.password.currentPassword")}</Label>
          <Input
            id="password-current"
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            disabled={change.isPending}
            aria-invalid={currentInvalid}
          />
        </Field>
        <Field>
          <Label htmlFor="password-new">{t("settings.user.password.newPassword")}</Label>
          <Input
            id="password-new"
            type="password"
            autoComplete="new-password"
            value={next}
            onChange={(e) => setNext(e.target.value)}
            disabled={change.isPending}
            aria-invalid={nextInvalid}
          />
        </Field>
        <Field>
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
            disabled={change.isPending}
            aria-invalid={mismatch ? true : undefined}
          />
        </Field>
        {mismatch && <FieldError>{t("settings.user.password.mismatch")}</FieldError>}
        {change.error && <FieldError>{errorMessage(t, change.error)}</FieldError>}
        <Button type="submit" disabled={!current || !next || change.isPending}>
          {change.isPending
            ? t("settings.user.password.submitting")
            : t("settings.user.password.submit")}
        </Button>
      </Form>
    </Section>
  );
}
