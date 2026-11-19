import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ApiError,
  commitOnboardingChoice,
  getAuthMethods,
  getMe,
  getOnboardingOptions,
  localLogin,
  localRegister,
  logout,
  type LocalRegisterRequest,
  type Me,
  type OnboardingChoiceRequest,
  type OnboardingOptions,
} from "./client";
import { browserLocale, i18n } from "@/i18n";

export const meKey = ["me"] as const;

// The session probe (contract: "A 401 means show the login screen"). A 401 is
// an answer, not a failure, so it resolves to null instead of throwing.
export function useMe() {
  return useQuery<Me | null, ApiError>({
    queryKey: meKey,
    queryFn: async () => {
      try {
        return await getMe();
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) return null;
        throw err;
      }
    },
    retry: false,
    staleTime: 60_000,
  });
}

export function useAuthMethods() {
  return useQuery({ queryKey: ["auth", "methods"], queryFn: getAuthMethods, staleTime: Infinity });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { email: string; password: string }>({
    mutationFn: localLogin,
    // Login answers 204 with no body; /me is the follow-up (contract).
    onSuccess: () => qc.invalidateQueries({ queryKey: meKey }),
  });
}

export function useRegister() {
  return useMutation<void, ApiError, LocalRegisterRequest>({ mutationFn: localRegister });
}

export const onboardingKey = ["onboarding", "options"] as const;

// The gate's own probe, as /me is the session's: a 401 means the handshake is
// gone (expired, spent or never opened) and the person starts again at sign-in.
// Not cached across visits: a handshake lives fifteen minutes, so a stale
// answer is worse than a second request.
export function useOnboardingOptions() {
  return useQuery<OnboardingOptions, ApiError>({
    queryKey: onboardingKey,
    queryFn: getOnboardingOptions,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  });
}

export function useCommitChoice() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, OnboardingChoiceRequest>({
    mutationFn: commitOnboardingChoice,
    // The commit answers 204 with a session cookie and no body; /me is the
    // follow-up, as after a login. Invalidated, not just refetched: the probe
    // on /onboarding has long since cached "signed out".
    onSuccess: () => qc.invalidateQueries({ queryKey: meKey }),
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation<void, ApiError>({
    mutationFn: logout,
    onSuccess: async () => {
      // A fetch still in flight would land after the clear below and put the
      // last User's answer back in the cache.
      await qc.cancelQueries();
      // The next person at this device is not the last User: back to the
      // browser's language until someone signs in.
      void i18n.changeLanguage(browserLocale());
      qc.setQueryData(meKey, null);
      qc.removeQueries({ predicate: (q) => q.queryKey[0] !== meKey[0] });
    },
  });
}
