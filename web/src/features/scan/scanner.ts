import {
  BarcodeDetector as PonyfillDetector,
  setZXingModuleOverrides,
} from "barcode-detector/ponyfill";
import wasmUrl from "zxing-wasm/reader/zxing_reader.wasm?url";

// The WASM decoder is served by this app (the library would otherwise fetch it from a CDN,
// which our Content-Security-Policy forbids and which would leak usage).
setZXingModuleOverrides({
  locateFile: (path: string, prefix: string) => (path.endsWith(".wasm") ? wasmUrl : prefix + path),
});

export interface Detector {
  detect(source: HTMLVideoElement): Promise<string[]>;
}

/** Uses the browser's native BarcodeDetector when available, else the WASM implementation. */
export function createDetector(): Detector {
  const native = (globalThis as { BarcodeDetector?: typeof PonyfillDetector }).BarcodeDetector;
  const Impl = native ?? PonyfillDetector;
  const d = new Impl({ formats: ["ean_13"] });
  return {
    async detect(video) {
      const found = await d.detect(video);
      return found.map((f) => f.rawValue);
    },
  };
}
