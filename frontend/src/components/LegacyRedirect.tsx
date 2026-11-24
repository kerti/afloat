import { Navigate, useLocation } from "react-router";

// An old screen's path, now a redirect. The query string and hash ride along,
// so an old ?edit=pocket:<id> link still opens its dialog.
export function LegacyRedirect({ to }: { to: string }) {
  const { search, hash } = useLocation();
  return <Navigate to={{ pathname: to, search, hash }} replace />;
}
