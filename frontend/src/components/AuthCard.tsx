import type { ReactNode } from "react";

// The frame for screens outside the Shell (login, register, onboarding, and
// the session probe's own loading and failure): one centred max-w-sm card,
// padded for the device's safe areas. Signed-in screens use Shell instead.
export function AuthCard({ children }: { children: ReactNode }) {
  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-sm flex-col justify-center gap-6 pt-[max(1rem,env(safe-area-inset-top))] pr-[max(1rem,env(safe-area-inset-right))] pb-[max(1rem,env(safe-area-inset-bottom))] pl-[max(1rem,env(safe-area-inset-left))]">
      {children}
    </main>
  );
}
