import { Navigate, Route, Routes } from "react-router";
import { RequireSession } from "@/components/RequireSession";
import { Toaster } from "@/components/ui/sonner";
import { UpdatePrompt } from "@/components/UpdatePrompt";
import { routes } from "@/lib/routes";
import { HomePage } from "@/pages/HomePage";
import { HouseholdSettingsPage } from "@/pages/HouseholdSettingsPage";
import { LoginPage } from "@/pages/LoginPage";
import { OnboardingPage } from "@/pages/OnboardingPage";
import { PocketsPage } from "@/pages/PocketsPage";
import { RegisterPage } from "@/pages/RegisterPage";
import { UserSettingsPage } from "@/pages/UserSettingsPage";

export function App() {
  return (
    <>
      <Routes>
        <Route path={routes.login} element={<LoginPage />} />
        <Route path={routes.register} element={<RegisterPage />} />
        <Route path={routes.onboarding} element={<OnboardingPage />} />
        <Route
          path={routes.home}
          element={<RequireSession>{(me) => <HomePage me={me} />}</RequireSession>}
        />
        <Route
          path={routes.householdSettings}
          element={<RequireSession>{(me) => <HouseholdSettingsPage me={me} />}</RequireSession>}
        />
        <Route
          path={routes.userSettings}
          element={<RequireSession>{(me) => <UserSettingsPage me={me} />}</RequireSession>}
        />
        <Route
          path={routes.pockets}
          element={<RequireSession>{() => <PocketsPage />}</RequireSession>}
        />
        <Route path="*" element={<Navigate to={routes.home} replace />} />
      </Routes>
      <UpdatePrompt />
      <Toaster />
    </>
  );
}
