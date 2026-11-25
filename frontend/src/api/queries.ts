import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ApiError,
  changeEmail,
  changePassword,
  commitOnboardingChoice,
  createCategory,
  createExpense,
  updateExpense,
  createPocket,
  deleteCategory,
  deletePocket,
  getAuthMethods,
  getBudget,
  getMe,
  getOnboardingOptions,
  listExpenses,
  listCategories,
  listPockets,
  localLogin,
  localRegister,
  logout,
  updateBudget,
  updateCategory,
  updatePocket,
  updateHousehold,
  updateMe,
  type Budget,
  type Category,
  type CategoryList,
  type CreateCategoryRequest,
  type ChangeEmailRequest,
  type ChangePasswordRequest,
  type CreateExpenseRequest,
  type UpdateExpenseRequest,
  type CreatePocketRequest,
  type Expense,
  type ExpenseList,
  type Household,
  type LocalRegisterRequest,
  type Me,
  type OnboardingChoiceRequest,
  type OnboardingOptions,
  type Pocket,
  type PocketList,
  type UpdateBudgetRequest,
  type UpdateCategoryRequest,
  type UpdateHouseholdRequest,
  type UpdateMeRequest,
  type UpdatePocketRequest,
} from "./client";
import { clearSession, meKey } from "./queryClient";

export { meKey };

// The session probe (contract: "A 401 means show the login screen"). A 401 is
// an answer, not a failure, so it resolves to null instead of throwing.
export function useMe() {
  return useQuery<Me | null, ApiError>({
    queryKey: meKey,
    queryFn: async () => {
      try {
        return await getMe();
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) return null;
        throw err;
      }
    },
    retry: false,
    staleTime: 60_000,
  });
}

export function useAuthMethods() {
  return useQuery({ queryKey: ["auth", "methods"], queryFn: getAuthMethods, staleTime: Infinity });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { email: string; password: string }>({
    mutationFn: localLogin,
    // Login answers 204 with no body; /me is the follow-up (contract).
    onSuccess: () => qc.invalidateQueries({ queryKey: meKey }),
  });
}

export function useRegister() {
  return useMutation<void, ApiError, LocalRegisterRequest>({ mutationFn: localRegister });
}

export const onboardingKey = ["onboarding", "options"] as const;

// The gate's own probe, as /me is the session's: a 401 means the handshake is
// gone (expired, spent or never opened) and the person starts again at sign-in.
// Not cached across visits: a handshake lives fifteen minutes, so a stale
// answer is worse than a second request.
export function useOnboardingOptions() {
  return useQuery<OnboardingOptions, ApiError>({
    queryKey: onboardingKey,
    queryFn: getOnboardingOptions,
    meta: { handshake: true },
    retry: false,
    staleTime: 0,
    gcTime: 0,
  });
}

export function useCommitChoice() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, OnboardingChoiceRequest>({
    mutationFn: commitOnboardingChoice,
    meta: { handshake: true },
    // The commit answers 204 with a session cookie and no body; /me is the
    // follow-up, as after a login. Invalidated, not just refetched: the probe
    // on /onboarding has long since cached "signed out".
    onSuccess: () => qc.invalidateQueries({ queryKey: meKey }),
  });
}

export function useUpdateHousehold() {
  const qc = useQueryClient();
  return useMutation<Household, ApiError, UpdateHouseholdRequest>({
    mutationFn: updateHousehold,
    // /me carries the Household and is the one read path: invalidate, don't
    // patch the cache by hand.
    onSuccess: () => qc.invalidateQueries({ queryKey: meKey }),
  });
}

// The answer is a Me, but /me is the one read path: invalidate rather than
// patch the cache. A locale change flips the language when RequireSession
// sees the refetched locale.
export function useUpdateMe() {
  const qc = useQueryClient();
  return useMutation<Me, ApiError, UpdateMeRequest>({
    mutationFn: updateMe,
    onSuccess: () => qc.invalidateQueries({ queryKey: meKey }),
  });
}

export function useChangeEmail() {
  const qc = useQueryClient();
  return useMutation<Me, ApiError, ChangeEmailRequest>({
    mutationFn: changeEmail,
    onSuccess: () => qc.invalidateQueries({ queryKey: meKey }),
  });
}

// The server rotates this session's cookie in the same transaction that
// commits the change, so a request still in flight with the old cookie answers
// 401 UNAUTHORIZED and would end the UI's session (#119 D6). Cancel everything
// first. Nothing in Me changes, so /me is not refetched.
export function useChangePassword() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, ChangePasswordRequest>({
    mutationFn: changePassword,
    onMutate: () => qc.cancelQueries(),
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation<void, ApiError>({
    mutationFn: logout,
    onSuccess: () => clearSession(qc),
  });
}

export const pocketsKey = ["pockets"] as const;

export function usePockets() {
  return useQuery<PocketList, ApiError>({ queryKey: pocketsKey, queryFn: listPockets });
}

// The list is the one read path, as /me is: invalidate, never patch the cache.
export function useCreatePocket() {
  const qc = useQueryClient();
  return useMutation<Pocket, ApiError, CreatePocketRequest>({
    mutationFn: createPocket,
    onSuccess: () => qc.invalidateQueries({ queryKey: pocketsKey }),
  });
}

// A NOT_FOUND means the list is stale (another member deleted the Pocket), so
// it is refetched whichever way the write went.
export function useUpdatePocket() {
  const qc = useQueryClient();
  return useMutation<Pocket, ApiError, { id: string; body: UpdatePocketRequest }>({
    mutationFn: ({ id, body }) => updatePocket(id, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: pocketsKey }),
    onError: (err) => {
      if (err.code === "NOT_FOUND") void qc.invalidateQueries({ queryKey: pocketsKey });
    },
  });
}

// Deleting is a soft delete the server can reverse (restorePocket, the Undo).
// A NOT_FOUND means the list is stale, so it is refetched.
export function useDeletePocket() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: deletePocket,
    onSuccess: () => qc.invalidateQueries({ queryKey: pocketsKey }),
    onError: (err) => {
      if (err.code === "NOT_FOUND") void qc.invalidateQueries({ queryKey: pocketsKey });
    },
  });
}

export const budgetKey = ["budget"] as const;

export function useBudget() {
  return useQuery<Budget, ApiError>({ queryKey: budgetKey, queryFn: getBudget });
}

// The answer is the whole Budget, but the query is the one read path:
// invalidate, never patch the cache. The promise is returned so the write
// settles after the figures have been refetched.
export function useUpdateBudget() {
  const qc = useQueryClient();
  return useMutation<Budget, ApiError, UpdateBudgetRequest>({
    mutationFn: updateBudget,
    onSuccess: () => qc.invalidateQueries({ queryKey: budgetKey }),
  });
}

export const categoriesKey = ["categories"] as const;

export function useCategories() {
  return useQuery<CategoryList, ApiError>({ queryKey: categoriesKey, queryFn: listCategories });
}

// A new Category may carry an allocation, which moves the Budget's figures, so
// both reads are invalidated and the write settles after they have refetched.
export function useCreateCategory() {
  const qc = useQueryClient();
  return useMutation<Category, ApiError, CreateCategoryRequest>({
    mutationFn: createCategory,
    onSuccess: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: categoriesKey }),
        qc.invalidateQueries({ queryKey: budgetKey }),
      ]),
  });
}

// A deactivated Category may hold an allocation, so both reads are refetched and
// the write settles after them. A NOT_FOUND means the list is stale.
export function useUpdateCategory() {
  const qc = useQueryClient();
  const refetch = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: categoriesKey }),
      qc.invalidateQueries({ queryKey: budgetKey }),
    ]);
  return useMutation<Category, ApiError, { id: string; body: UpdateCategoryRequest }>({
    mutationFn: ({ id, body }) => updateCategory(id, body),
    onSuccess: refetch,
    onError: (err) => {
      if (err.code === "NOT_FOUND") void refetch();
    },
  });
}

// A deleted Category drops out of every figure at once, so both reads are
// refetched and the write settles after them. A NOT_FOUND means the lists are stale.
export function useDeleteCategory() {
  const qc = useQueryClient();
  const refetch = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: categoriesKey }),
      qc.invalidateQueries({ queryKey: budgetKey }),
    ]);
  return useMutation<void, ApiError, string>({
    mutationFn: deleteCategory,
    // Not awaited: the caller closes the dialog on the 204, before the refetched
    // list drops the Category and unmounts the dialog under a stale ?edit=.
    onSuccess: () => void refetch(),
    onError: (err) => {
      if (err.code === "NOT_FOUND") void refetch();
    },
  });
}

export const expensesKey = ["expenses"] as const;

// Keyset paging: each page's `next` is the following request's `after`, and a
// page without one is the last. Pages are only ever appended on demand, never
// fetched ahead.
export function useExpenses() {
  return useInfiniteQuery<
    ExpenseList,
    ApiError,
    { pages: ExpenseList[] },
    readonly string[],
    string | undefined
  >({
    queryKey: expensesKey,
    queryFn: ({ pageParam }) => listExpenses(pageParam),
    initialPageParam: undefined,
    getNextPageParam: (last) => last.next,
  });
}

// A change to an Expense moves the list and, through the Pocket, the default.
export function useUpdateExpense() {
  const qc = useQueryClient();
  return useMutation<Expense, ApiError, { id: string; body: UpdateExpenseRequest }>({
    mutationFn: ({ id, body }) => updateExpense(id, body),
    onSuccess: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: expensesKey }),
        qc.invalidateQueries({ queryKey: pocketsKey }),
      ]),
  });
}

// A new Expense moves the list and the default Pocket (the Pockets read carries
// it), so both are invalidated and the write settles after they have refetched.
export function useCreateExpense() {
  const qc = useQueryClient();
  return useMutation<Expense, ApiError, CreateExpenseRequest>({
    mutationFn: createExpense,
    onSuccess: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: expensesKey }),
        qc.invalidateQueries({ queryKey: pocketsKey }),
      ]),
  });
}
