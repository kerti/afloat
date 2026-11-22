import { render } from "@testing-library/react";
import { MemoryRouter, type NavigateFunction } from "react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { createQueryClient } from "@/api/queryClient";
import { App } from "@/App";
import { Probe } from "./Probe";

export function renderApp(path = "/") {
  const client = createQueryClient({ defaultOptions: { queries: { retry: false } } });
  let navigateFn: NavigateFunction | null = null;
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <App />
        <Probe
          onNavigate={(n) => {
            navigateFn = n;
          }}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return {
    ...view,
    client,
    navigate: (delta: number) => navigateFn!(delta),
    // The search param's value, or null.
    param: (name: string) =>
      new URLSearchParams(view.getByTestId("location").textContent!.split("?")[1] ?? "").get(name),
  };
}
