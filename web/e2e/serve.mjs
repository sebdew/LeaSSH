// Lance le binaire Leassh sur une base neuve pour les tests de bout en bout.
// Prérequis : `make build` (le binaire embarque le frontend).
import { spawn } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomBytes } from "node:crypto";

const dataDir = mkdtempSync(join(tmpdir(), "leassh-e2e-"));
const child = spawn("../bin/leassh", ["serve"], {
  stdio: "inherit",
  env: {
    PATH: process.env.PATH,
    LEASSH_DATA_DIR: dataDir,
    LEASSH_LISTEN: "127.0.0.1:18080",
    LEASSH_MASTER_KEY: randomBytes(32).toString("base64"),
    LEASSH_SETUP_TOKEN: "e2e-setup-token",
  },
});
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => child.kill(sig));
child.on("exit", (code) => process.exit(code ?? 0));
