// Thin wrappers around the WebAuthn JSON helpers of current browsers. The server sends the
// options as JSON; the browser turns them into the binary form and back.

interface JsonCapable {
  parseCreationOptionsFromJSON(o: object): PublicKeyCredentialCreationOptions;
  parseRequestOptionsFromJSON(o: object): PublicKeyCredentialRequestOptions;
}

function api(): JsonCapable | null {
  const pk = (globalThis as { PublicKeyCredential?: unknown }).PublicKeyCredential as
    | (Partial<JsonCapable> & object)
    | undefined;
  if (
    pk &&
    typeof pk.parseCreationOptionsFromJSON === "function" &&
    typeof pk.parseRequestOptionsFromJSON === "function"
  ) {
    return pk as JsonCapable;
  }
  return null;
}

export const passkeysSupported = () => api() !== null;

function inner(options: unknown): object {
  const o = options as { publicKey?: object };
  return o.publicKey ?? (options as object);
}

export async function createPasskey(options: unknown): Promise<object> {
  const a = api();
  if (!a) throw new Error("passkeys are not supported by this browser");
  const cred = (await navigator.credentials.create({
    publicKey: a.parseCreationOptionsFromJSON(inner(options)),
  })) as (PublicKeyCredential & { toJSON(): object }) | null;
  if (!cred) throw new Error("cancelled");
  return cred.toJSON();
}

export async function getPasskey(options: unknown): Promise<object> {
  const a = api();
  if (!a) throw new Error("passkeys are not supported by this browser");
  const cred = (await navigator.credentials.get({
    publicKey: a.parseRequestOptionsFromJSON(inner(options)),
  })) as (PublicKeyCredential & { toJSON(): object }) | null;
  if (!cred) throw new Error("cancelled");
  return cred.toJSON();
}
