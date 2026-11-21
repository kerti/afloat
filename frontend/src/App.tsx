import { Navigate, Route, Routes } from "react-router";
import { RequireSession } from "@/components/RequireSession";
import { UpdatePrompt } from "@/components/UpdatePrompt";
import { HomePage } from "@/pages/HomePage";
import { HouseholdSettingsPage } from "@/pages/HouseholdSettingsPage";
import { LoginPage } from "@/pages/LoginPage";
import { OnboardingPage } from "@/pages/OnboardingPage";
import { RegisterPage } from "@/pages/RegisterPage";
import { UserSettingsPage } from "@/pages/UserSettingsPage";

export function App() {
  return (
    <>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route path="/onboarding" element={<OnboardingPage />} />
        <Route path="/" element={<RequireSession>{(me) => <HomePage me={me} />}</RequireSession>} />
        <Route
          path="/settings/household"
          element={<RequireSession>{(me) => <HouseholdSettingsPage me={me} />}</RequireSession>}
        />
        <Route
          path="/settings/user"
          element={<RequireSession>{(me) => <UserSettingsPage me={me} />}</RequireSession>}
        />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      <UpdatePrompt />
    </>
  );
}
