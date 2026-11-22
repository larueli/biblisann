// Package webui serves the built single-page application embedded in the binary.
//
// The Vite build writes to dist/. Only dist/.gitkeep is committed so the package compiles
// before the first web build (the handler then answers 503).
package webui
