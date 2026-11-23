/** Decodes a base64url string (the VAPID public key) into the bytes the Push API wants. */
export function urlBase64ToUint8Array(b64url: string): Uint8Array<ArrayBuffer> {
  const pad = "=".repeat((4 - (b64url.length % 4)) % 4);
  const raw = atob((b64url + pad).replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

export const pushSupported = () =>
  "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;

export interface PushSubscriptionData {
  endpoint: string;
  p256dh: string;
  auth: string;
}

/** Asks permission and subscribes this browser to the server's push messages. */
export async function subscribePush(vapidPublicKey: string): Promise<PushSubscriptionData> {
  if ((await Notification.requestPermission()) !== "granted")
    throw new Error("notifications were not allowed");
  const registration = await navigator.serviceWorker.ready;
  const sub = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(vapidPublicKey),
  });
  const json = sub.toJSON();
  if (!json.endpoint || !json.keys?.p256dh || !json.keys.auth)
    throw new Error("the browser returned an incomplete subscription");
  return { endpoint: json.endpoint, p256dh: json.keys.p256dh, auth: json.keys.auth };
}
