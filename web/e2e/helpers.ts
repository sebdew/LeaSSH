import { execFileSync } from "node:child_process";
import net from "node:net";
/* eslint-disable @typescript-eslint/no-explicit-any -- réponses JSON de l'API, non typées côté test */
import { expect, type Page } from "@playwright/test";

export const admin = { user: "admin", password: "Correct-Horse-Battery-9" };
export const base = "http://127.0.0.1:18080";

export async function login(page: Page) {
  await page.goto("/");
  await page.getByLabel("Nom d'utilisateur").fill(admin.user);
  await page.getByLabel("Mot de passe").fill(admin.password);
  await page.getByRole("button", { name: "Se connecter" }).click();
  await expect(page.getByRole("link", { name: "Clés MCP" })).toBeVisible();
}

// Appel de l'API d'administration avec la session et le jeton CSRF de la page connectée.
export async function api<T = Record<string, any>>(page: Page, method: "GET" | "POST" | "PUT" | "DELETE", path: string, body?: unknown): Promise<T> {
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  const res = await page.request.fetch(`/api/v1${path}`, {
    method, data: body, headers: { "X-CSRF-Token": me.csrf_token ?? "" },
  });
  expect(res.status(), `${method} ${path} : ${await res.text()}`).toBeLessThan(300);
  return (res.status() === 204 ? undefined : await res.json()) as T;
}

// --- sshd de test (même image figée que les tests d'intégration Go) ---
const SSHD_IMAGE = process.env.LEASSH_TEST_SSHD_IMAGE ?? "lscr.io/linuxserver/openssh-server@sha256:aa4e1b3916e8555f24e574e6108b0b89a1b20362b0a11800a71280b45999649e";

export function dockerAvailable(): boolean {
  try { execFileSync("docker", ["version", "--format", "{{.Server.Version}}"], { stdio: "ignore" }); return true; } catch { return false; }
}

export async function startSshd(publicKey: string): Promise<{ port: number; stop: () => void }> {
  const id = execFileSync("docker", ["run", "-d", "-p", "127.0.0.1::2222", "-e", `PUBLIC_KEY=${publicKey.trim()}`, "-e", "USER_NAME=leassh",
    "-e", "PASSWORD_ACCESS=false", "-e", "SUDO_ACCESS=false", SSHD_IMAGE], { encoding: "utf8", timeout: 240_000 }).trim();
  const stop = () => { try { execFileSync("docker", ["rm", "-f", id], { stdio: "ignore" }); } catch { /* déjà supprimé */ } };
  const mapped = execFileSync("docker", ["port", id, "2222/tcp"], { encoding: "utf8" }).trim().split("\n")[0];
  const port = Number(mapped.slice(mapped.lastIndexOf(":") + 1));
  const deadline = Date.now() + 180_000;
  while (Date.now() < deadline) {
    if (await banner(port)) return { port, stop };
    await new Promise((r) => setTimeout(r, 1000));
  }
  stop();
  throw new Error("le sshd de test n'a pas démarré");
}

function banner(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    let done = false;
    const s = net.connect({ port, host: "127.0.0.1" });
    const end = (ok: boolean) => { if (!done) { done = true; clearTimeout(timer); s.destroy(); resolve(ok); } };
    const timer = setTimeout(() => end(false), 2000);
    s.on("connect", () => s.write("SSH-2.0-e2e-probe\r\n")); // ce sshd répond après l'identification du client
    s.on("data", (d) => end(d.toString().startsWith("SSH-")));
    s.on("error", () => end(false));
    s.on("close", () => end(false));
  });
}

// --- client MCP minimal (HTTP streamable, JSON-RPC) ---
export interface ToolResult { isError: boolean; data: any; text: string }

export async function mcpClient(token: string) {
  let session = "";
  let n = 0;
  const headers = () => ({
    "Content-Type": "application/json", Accept: "application/json, text/event-stream", Authorization: `Bearer ${token}`,
    ...(session ? { "Mcp-Session-Id": session } : {}),
  });
  const rpc = async (method: string, params: unknown, notify = false) => {
    const res = await fetch(`${base}/mcp`, { method: "POST", headers: headers(), body: JSON.stringify({ jsonrpc: "2.0", ...(notify ? {} : { id: ++n }), method, params }) });
    const sid = res.headers.get("Mcp-Session-Id");
    if (sid) session = sid;
    const text = await res.text();
    if (notify) return undefined;
    if (!res.ok) throw new Error(`MCP ${method} : HTTP ${res.status} ${text}`);
    const payload = res.headers.get("Content-Type")?.includes("text/event-stream")
      ? text.split("\n").filter((l) => l.startsWith("data:")).map((l) => JSON.parse(l.slice(5)))[0] : JSON.parse(text);
    if (payload.error) throw new Error(`MCP ${method} : ${payload.error.message}`);
    return payload.result;
  };
  await rpc("initialize", { protocolVersion: "2025-03-26", capabilities: {}, clientInfo: { name: "e2e", version: "0" } });
  await rpc("notifications/initialized", {}, true);
  return {
    async call(tool: string, args: Record<string, unknown> = {}): Promise<ToolResult> {
      const r = await rpc("tools/call", { name: tool, arguments: args });
      const text: string = r.content?.[0]?.text ?? "";
      let data = r.structuredContent;
      if (data === undefined) { try { data = JSON.parse(text); } catch { data = undefined; } }
      return { isError: !!r.isError, data, text };
    },
    async tools(): Promise<string[]> { return (await rpc("tools/list", {})).tools.map((t: { name: string }) => t.name); },
  };
}
