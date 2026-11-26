import { Navigate, Route, Routes } from "react-router";
import { AppearanceProvider } from "@/lib/appearance";
import { RequireSession } from "@/components/RequireSession";
import { Toaster } from "@/components/ui/sonner";
import { UpdatePrompt } from "@/components/UpdatePrompt";
import { LegacyRedirect } from "@/components/LegacyRedirect";
import { legacyRedirects, routes } from "@/lib/routes";
import { BudgetPage } from "@/pages/BudgetPage";
import { CapturePage } from "@/pages/CapturePage";
import { EditExpensePage } from "@/pages/EditExpensePage";
import { ExpensesPage } from "@/pages/ExpensesPage";
import { RecycleBinPage } from "@/pages/RecycleBinPage";
import { HomePage } from "@/pages/HomePage";
import { LoginPage } from "@/pages/LoginPage";
import { OnboardingPage } from "@/pages/OnboardingPage";
import { RegisterPage } from "@/pages/RegisterPage";
import { SettingsPage } from "@/pages/SettingsPage";

export function App() {
  return (
    <AppearanceProvider>
      <Routes>
        <Route path={routes.login} element={<LoginPage />} />
        <Route path={routes.register} element={<RegisterPage />} />
        <Route path={routes.onboarding} element={<OnboardingPage />} />
        <Route
          path={routes.home}
          element={<RequireSession>{(me) => <HomePage me={me} />}</RequireSession>}
        />
        <Route
          path={routes.settings}
          element={<RequireSession>{(me) => <SettingsPage me={me} />}</RequireSession>}
        />
        {Object.values(legacyRedirects).map((path) => (
          <Route key={path} path={path} element={<LegacyRedirect to={routes.settings} />} />
        ))}
        <Route
          path={routes.budget}
          element={<RequireSession>{(me) => <BudgetPage me={me} />}</RequireSession>}
        />
        <Route
          path={routes.capture}
          element={<RequireSession>{(me) => <CapturePage me={me} />}</RequireSession>}
        />
        <Route
          path={routes.expenses}
          element={<RequireSession>{(me) => <ExpensesPage me={me} />}</RequireSession>}
        />
        <Route
          path={routes.recycleBin}
          element={<RequireSession>{(me) => <RecycleBinPage me={me} />}</RequireSession>}
        />
        <Route
          path={routes.expensePattern}
          element={<RequireSession>{(me) => <EditExpensePage me={me} />}</RequireSession>}
        />
        <Route path="*" element={<Navigate to={routes.home} replace />} />
      </Routes>
      <UpdatePrompt />
      <Toaster />
    </AppearanceProvider>
  );
}
