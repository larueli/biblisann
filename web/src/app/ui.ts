// Shared class names, so screens stay consistent. They come from the shadcn/ui components
// (src/components/ui), which makes every existing screen follow them.
import { buttonVariants } from "../components/ui/button";
import { inputClass } from "../components/ui/input";

export const field = inputClass;
export const primary = buttonVariants({ variant: "default" });
export const secondary = buttonVariants({ variant: "outline" });
export const muted = "text-sm text-slate-600 dark:text-slate-300";
