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
  updateHousehold,
  type Household,
  type LocalRegisterRequest,
  type Me,
  type OnboardingChoiceRequest,
  type OnboardingOptions,
  type UpdateHouseholdRequest,
} from "./client";
import { clearSession, meKey } from "./queryClient";

export { meKey };

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
    meta: { handshake: true },
    retry: false,
    staleTime: 0,
    gcTime: 0,
  });
}

export function useCommitChoice() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, OnboardingChoiceRequest>({
    mutationFn: commitOnboardingChoice,
    meta: { handshake: true },
    // The commit answers 204 with a session cookie and no body; /me is the
    // follow-up, as after a login. Invalidated, not just refetched: the probe
    // on /onboarding has long since cached "signed out".
    onSuccess: () => qc.invalidateQueries({ queryKey: meKey }),
  });
}

export function useUpdateHousehold() {
  const qc = useQueryClient();
  return useMutation<Household, ApiError, UpdateHouseholdRequest>({
    mutationFn: updateHousehold,
    // /me carries the Household and is the one read path: invalidate, don't
    // patch the cache by hand.
    onSuccess: () => qc.invalidateQueries({ queryKey: meKey }),
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation<void, ApiError>({
    mutationFn: logout,
    onSuccess: () => clearSession(qc),
  });
}
