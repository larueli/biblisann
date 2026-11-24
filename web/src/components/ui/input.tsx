import type * as React from "react";
import { cn } from "../../lib/utils";

export const inputClass =
  "w-full rounded border border-slate-400 bg-transparent px-3 py-2 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-slate-500 disabled:opacity-50";

export function Input({ className, ...props }: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cn(inputClass, className)} {...props} />;
}
