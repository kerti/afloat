import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { createQueryClient } from "@/api/queryClient";
import { App } from "@/App";

export function renderApp(path = "/") {
  const client = createQueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...view, client };
}
