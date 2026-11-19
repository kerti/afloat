import { Navigate, Route, Routes } from "react-router";
import { RequireSession } from "@/components/RequireSession";
import { UpdatePrompt } from "@/components/UpdatePrompt";
import { HomePage } from "@/pages/HomePage";
import { LoginPage } from "@/pages/LoginPage";

export function App() {
  return (
    <>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/" element={<RequireSession>{(me) => <HomePage me={me} />}</RequireSession>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      <UpdatePrompt />
    </>
  );
}
