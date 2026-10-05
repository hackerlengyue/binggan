import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "饼干大小姐",
  identifier: process.env.BINGGAN_DEV_IDENTIFIER || "time.binggan.haomen.v2",
  version: "2.0.0",
  devUrl: "http://127.0.0.1:5175",
  devCommand: "npm --prefix frontend run dev",
  buildCommand: "npm --prefix frontend run build",
  frontendDist: "frontend/dist",
  bindings: "frontend/src/mygo.ts",
  out: "build",
  macos: { minimumSystemVersion: "13.0" },
});
