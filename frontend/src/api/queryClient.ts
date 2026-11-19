import {
  MutationCache,
  QueryCache,
  QueryClient,
  type QueryClientConfig,
} from "@tanstack/react-query";
import { ApiError } from "./client";
import { browserLocale, i18n } from "@/i18n";

export const meKey = ["me"] as const;

// Everything the last User left behind. Sign-out and a session that ended on
// its own (expiry, revocation) both run this, so they cannot drift apart.
export async function clearSession(qc: QueryClient): Promise<void> {
  // A fetch still in flight would land after the clear below and put the
  // last User's answer back in the cache.
  await qc.cancelQueries();
  // The next person at this device is not the last User: back to the
  // browser's language until someone signs in.
  void i18n.changeLanguage(browserLocale());
  qc.setQueryData(meKey, null);
  qc.removeQueries({ predicate: (q) => q.queryKey[0] !== meKey[0] });
}

// Only UNAUTHORIZED means "no valid session". Login's wrong password is also
// a 401 but carries INVALID_CREDENTIALS: nobody was signed in, nothing to clear.
const sessionEnded = (err: unknown) =>
  err instanceof ApiError && err.status === 401 && err.code === "UNAUTHORIZED";

export function createQueryClient(config: QueryClientConfig = {}): QueryClient {
  let hadUser = false;
  const end = () => {
    hadUser = false;
    void clearSession(client);
  };
  const client: QueryClient = new QueryClient({
    ...config,
    queryCache: new QueryCache({
      onError: (err) => {
        if (sessionEnded(err)) end();
      },
      // useMe turns a 401 into null, so /me never reaches onError: a User
      // becoming null is how its session ending shows up here. setQueryData
      // (sign-out) does not fire this, and the clear leaves /me alone, so
      // there is no refetch to loop on.
      onSuccess: (data, query) => {
        if (query.queryKey[0] !== meKey[0]) return;
        if (data) hadUser = true;
        else if (hadUser) end();
      },
    }),
    mutationCache: new MutationCache({
      onError: (err) => {
        if (sessionEnded(err)) end();
      },
    }),
  });
  return client;
}
