// Step-up authentication (ADR 0026). When the server answers 403 "step_up_required", the API
// client asks the user to prove themselves again (see StepUpDialog) and replays the request.

type Listener = (open: boolean) => void;

let waiting: Array<(ok: boolean) => void> = [];
const listeners = new Set<Listener>();

/** Opens the dialog (once, however many requests wait) and resolves when it is closed. */
export function requestStepUp(): Promise<boolean> {
  return new Promise((resolve) => {
    waiting.push(resolve);
    if (waiting.length === 1) for (const l of listeners) l(true);
  });
}

/** Called by the dialog: true after a successful re-authentication, false when cancelled. */
export function settleStepUp(ok: boolean): void {
  const done = waiting;
  waiting = [];
  for (const l of listeners) l(false);
  for (const resolve of done) resolve(ok);
}

export function subscribeStepUp(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export const stepUpRequiredDetail = "step_up_required";
