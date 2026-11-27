import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { i18n } from "@/i18n";
import enGB from "@/locales/en-GB.json";
import { ChartCard } from "@/components/ChartCard";

// The lazy chunk can fail to arrive (offline on a first visit, a deploy that
// retired the file the page asks for). Home must keep its figures, and the
// table must still say what the picture would have.
vi.mock("@/components/ChartCardImpl", () => {
  throw new Error("Failed to fetch dynamically imported module");
});

describe("a chart whose chunk cannot be loaded", () => {
  it("says so in a sentence, keeps its table and takes nothing else down", async () => {
    // React logs the rethrown error it caught at the boundary.
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});
    render(
      <I18nextProvider i18n={i18n}>
        <p>the rest of Home</p>
        <ChartCard
          points={[
            { date: "2026-11-01", target: "100000.0000", actual: "0.0000", in_progress: true },
          ]}
          currency="IDR"
          locale="en-GB"
        />
      </I18nextProvider>,
    );
    expect(await screen.findByText(enGB.dashboard.chart.unavailable)).toBeInTheDocument();
    expect(screen.getByText("the rest of Home")).toBeInTheDocument();
    expect(screen.getByRole("table", { name: enGB.dashboard.chart.label })).toBeInTheDocument();
    logged.mockRestore();
  });
});
