// The running build, a muted line under the app's name in the Shell's header,
// so which release a server runs can be read from any screen of a phone
// (Uruni web/src/components/AppVersion.tsx, github.com/kerti/uruni).
export function AppVersion() {
  return (
    <p className="truncate text-xs leading-4 text-muted-foreground tabular-nums">
      {__APP_VERSION__}
    </p>
  );
}
