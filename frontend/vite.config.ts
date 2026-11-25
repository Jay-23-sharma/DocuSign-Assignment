import { defineConfig } from "vite";

import { viteStaticCopy } from "vite-plugin-static-copy";

export default defineConfig({
  plugins: [viteStaticCopy({
    targets: ["cmaps", "standard_fonts", "wasm", "iccs"].map(name => ({
      src: "node_modules/pdfjs-dist/" + name,
      dest: "pdf-assets",
    })),
  })],
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: { "/api": "http://127.0.0.1:8080" },
  },
});
