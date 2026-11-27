import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useChoice } from "@/lib/useChoice";

vi.mock("sonner", async () => (await import("@/test/sonnerMock")).sonnerMock());
const toastMock = vi.mocked(toast);

// A write the test settles by hand.
function deferred() {
  let resolve!: () => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

beforeEach(() => vi.clearAllMocks());

describe("useChoice", () => {
  it("lets only the latest of two quick choices settle the control", async () => {
    const a = deferred();
    const b = deferred();
    const write = vi.fn().mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
    const { result } = renderHook(() => useChoice<string>("IDR", write));

    act(() => result.current[1]("USD"));
    act(() => result.current[1]("EUR"));
    expect(result.current[0]).toBe("EUR");

    // A fails late: no flicker back to the stored value, and no error toast
    // about a choice already replaced.
    await act(async () => {
      a.reject(new ApiError(400, "VALIDATION"));
      await a.promise.catch(() => {});
    });
    expect(result.current[0]).toBe("EUR");
    expect(toastMock.error).not.toHaveBeenCalled();

    await act(async () => {
      b.resolve();
      await b.promise;
    });
    expect(toastMock).toHaveBeenCalledTimes(1);
    expect(toastMock.error).not.toHaveBeenCalled();
  });

  it("still toasts a rejection that is not an ApiError", async () => {
    const write = vi.fn().mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() => useChoice<string>("en-GB", write));
    await act(async () => {
      result.current[1]("id-ID");
      await Promise.resolve();
    });
    await vi.waitFor(() => expect(toastMock.error).toHaveBeenCalledTimes(1));
    expect(result.current[0]).toBe("en-GB");
  });

  it("holds a saved choice until the stored value catches up, never flickering back", async () => {
    const a = deferred();
    const write = vi.fn().mockReturnValueOnce(a.promise);
    const { result, rerender } = renderHook(({ stored }) => useChoice<string>(stored, write), {
      initialProps: { stored: "adaptive" },
    });

    act(() => result.current[1]("fixed"));
    // The write answers before the refetched stored value reaches the hook.
    await act(async () => {
      a.resolve();
      await a.promise;
    });
    expect(result.current[0]).toBe("fixed");

    rerender({ stored: "fixed" });
    expect(result.current[0]).toBe("fixed");
    // Settled: a later change of the stored value (another member) shows.
    rerender({ stored: "adaptive" });
    expect(result.current[0]).toBe("adaptive");
  });
});
