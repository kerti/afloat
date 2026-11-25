import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { IconContext } from "@phosphor-icons/react";
import { initI18n } from "@/i18n";
import { createQueryClient } from "@/api/queryClient";
import { App } from "@/App";
import "@/styles/index.css";

initI18n();
const queryClient = createQueryClient();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {/* Every icon is duotone, set once (Uruni web/src/main.tsx). */}
    <IconContext.Provider value={{ weight: "duotone", size: 24 }}>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </QueryClientProvider>
    </IconContext.Provider>
  </StrictMode>,
);
