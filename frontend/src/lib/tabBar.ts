// What sits at the bottom of the screen clears the fixed tab bar, its raised
// Capture square included, and the device's bottom inset: the same 4rem + 2rem
// main reserves. Toasts and the update prompt both use it (#153).
export const clearOfTabBar = "calc(env(safe-area-inset-bottom) + 4rem + 2rem)";
