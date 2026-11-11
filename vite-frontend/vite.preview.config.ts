import { defineConfig, mergeConfig } from "vite";
import base from "./vite.config";
// 纯 JS 数据模块，不参与 tsc 构建
import { token, routes } from "./mock/preview-data.mjs";

/**
 * 本地预览配置：不连后端，用假数据把界面填满，只看样式。
 *   npm run dev:preview   →  http://localhost:3100
 * 只在开发服务器生效（apply: 'serve'），不影响 npm run build 的产物。
 */
export default mergeConfig(
  base,
  defineConfig({
    server: { port: 3100 },
    plugins: [
      {
        name: "fz-mock-preview",
        apply: "serve",
        transformIndexHtml(html: string) {
          const seed = `<script>(function(){try{if(!localStorage.getItem('token')){localStorage.setItem('token',${JSON.stringify(token)});localStorage.setItem('role_id','0');localStorage.setItem('admin','true');localStorage.setItem('name','admin');}}catch(e){}})();</script>`;
          return html.replace("<head>", `<head>${seed}`);
        },
        configureServer(server: any) {
          server.middlewares.use((req: any, res: any, next: any) => {
            const url: string = (req.url || "").split("?")[0];
            if (!url.startsWith("/api/v1/")) return next();
            const path = url.slice("/api/v1".length);
            const data = path in routes ? (routes as any)[path] : null;
            res.setHeader("Content-Type", "application/json; charset=utf-8");
            res.end(JSON.stringify({ code: 0, msg: "ok", data }));
          });
        },
      },
    ],
  })
);
