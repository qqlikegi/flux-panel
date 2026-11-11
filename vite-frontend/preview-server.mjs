/**
 * 本地静态预览服务：dist + 假接口。不依赖任何包，崩不了。
 *   node preview-server.mjs      →  http://localhost:3100
 * 只有看样式用；数据全是 mock/preview-data.mjs 里的假数据。
 */
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { token, routes } from "./mock/preview-data.mjs";

const ROOT = path.join(path.dirname(fileURLToPath(import.meta.url)), "dist");
const PORT = Number(process.env.PORT || 3100);

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
};

const seed = `<script>(function(){try{if(!localStorage.getItem('token')){localStorage.setItem('token',${JSON.stringify(token)});localStorage.setItem('role_id','0');localStorage.setItem('admin','true');localStorage.setItem('name','admin');}}catch(e){}})();</script>`;

function sendJson(res, data) {
  res.writeHead(200, { "Content-Type": "application/json; charset=utf-8" });
  res.end(JSON.stringify({ code: 0, msg: "ok", data }));
}

const server = http.createServer((req, res) => {
  const url = (req.url || "/").split("?")[0];

  if (url.startsWith("/api/v1/")) {
    const p = url.slice("/api/v1".length);
    return sendJson(res, p in routes ? routes[p] : null);
  }

  let file = path.join(ROOT, decodeURIComponent(url));
  if (!file.startsWith(ROOT)) {
    res.writeHead(403);
    return res.end("forbidden");
  }

  if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) {
    // SPA 回退
    file = path.join(ROOT, "index.html");
  }

  const ext = path.extname(file).toLowerCase();
  let body = fs.readFileSync(file);
  if (ext === ".html") {
    body = Buffer.from(body.toString("utf8").replace("<head>", `<head>${seed}`));
  }
  res.writeHead(200, { "Content-Type": MIME[ext] || "application/octet-stream" });
  res.end(body);
});

server.on("clientError", (_e, socket) => socket.destroy());
server.listen(PORT, () => {
  console.log(`preview ready → http://localhost:${PORT}  (dist: ${ROOT})`);
});
