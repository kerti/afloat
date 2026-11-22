import { useCallback, useEffect, useRef } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";

// A dialog's open state lives in a search param, so the back button closes it
// and a link opens it (Uruni ADR-032/046; web/src/lib/useDialogParam.ts,
// github.com/kerti/uruni). open() pushes an entry tagged in location.state; a
// close() on a tagged entry goes back, so the X, Esc and the back gesture land
// in the same place. An untagged entry (a deep link) has the param stripped in
// place instead, so close never leaves the app.
type DialogState = { dialog?: boolean };

export function useDialogParam(param: string) {
  const [searchParams, setSearchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const value = searchParams.get(param);

  // close() is often called from a write's .then, long after the click. By
  // then the user may have dismissed the dialog (X, Esc, back) or left the
  // screen; a navigate(-1) captured at click time would then go back a second
  // time and leave the screen. So close() reads the latest render, and does
  // nothing once the param is gone or the screen has unmounted.
  const tagged = (location.state as DialogState | null)?.dialog === true;
  const latest = useRef({ value, tagged });
  useEffect(() => {
    latest.current = { value, tagged };
  }, [value, tagged]);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const open = useCallback(
    (next: string) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          params.set(param, next);
          return params;
        },
        { state: { dialog: true } satisfies DialogState },
      );
    },
    [param, setSearchParams],
  );

  // Strips the param in place, never through history: for a param naming
  // something that does not exist, where a second navigate(-1) could leave the
  // screen.
  const clear = useCallback(() => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.delete(param);
        return params;
      },
      { replace: true },
    );
  }, [param, setSearchParams]);

  const close = useCallback(() => {
    if (!mounted.current || latest.current.value === null) return;
    if (latest.current.tagged) {
      void navigate(-1);
      return;
    }
    clear();
  }, [clear, navigate]);

  return { value, open, close, clear };
}
