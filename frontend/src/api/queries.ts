import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, getAuthMethods, getMe, localLogin, logout, type Me } from "./client";
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

export function useLogout() {
  const qc = useQueryClient();
  return useMutation<void, ApiError>({
    mutationFn: logout,
    onSuccess: () => {
      // The next person at this device is not the last User: back to the
      // browser's language until someone signs in.
      void i18n.changeLanguage(browserLocale());
      qc.setQueryData(meKey, null);
      qc.removeQueries({ predicate: (q) => q.queryKey[0] !== meKey[0] });
    },
  });
}
