import type { Command, McpKey, Rule, SshServer } from "./types";

// Un objet désactivé, révoqué ou expiré n'est jamais proposé dans un sélecteur: on ne peut pas s'y associer par erreur.
export const keyActive = (k: McpKey) => !k.disabled && (!k.expires_at || new Date(k.expires_at) > new Date());
export const serverActive = (s: SshServer) => s.enabled;
export const commandActive = (c: Command) => c.status === "enabled";
export const ruleActive = (r: Rule) => r.enabled;
