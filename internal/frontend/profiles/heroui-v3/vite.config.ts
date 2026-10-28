import { readFileSync } from "node:fs";
import { randomBytes } from "node:crypto";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const target = new URL(
  process.env.<%.Prefix%>WEB_API_TARGET ?? "http://127.0.0.1:8080",
);
if (
  target.protocol !== "http:" ||
  target.hostname !== "127.0.0.1" ||
  target.pathname !== "/" ||
  target.username ||
  target.password ||
  target.search ||
  target.hash
) {
  throw new Error(
    "Development API target must be plain HTTP on numeric loopback",
  );
}
const proxy = Object.fromEntries(
  ["/api", "/auth", "/healthz"].map((path) => [
    path,
    { target: target.origin, changeOrigin: false },
  ]),
);
const https = () => ({
  key: readFileSync(new URL("./.cert/key.pem", import.meta.url)),
  cert: readFileSync(new URL("./.cert/cert.pem", import.meta.url)),
});

const nonce = randomBytes(18).toString("base64");
export default defineConfig(({ command, isPreview }) => ({
  html: { cspNonce: command === "serve" && !isPreview ? nonce : undefined },
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "local-development-csp",
      transformIndexHtml: {
        order: "post",
        handler(html) {
          return command === "serve" && !isPreview
            ? html.replace(
                "script-src 'self'",
                `script-src 'self' 'nonce-${nonce}'`,
              )
            : html;
        },
      },
    },
  ],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    https: command === "serve" && !process.env.VITEST ? https() : undefined,
    proxy,
    headers: {
      "X-Content-Type-Options": "nosniff",
      "Referrer-Policy": "no-referrer",
    },
  },
  preview: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    https: isPreview ? https() : undefined,
    proxy,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    restoreMocks: true,
  },
}));
