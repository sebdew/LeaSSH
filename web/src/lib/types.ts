export type Risk = "read" | "write" | "destructive";
export type ParamType = "enum" | "int" | "identifier" | "service" | "path" | "regex";

export interface ParamDef {
  type: ParamType;
  description?: string;
  required?: boolean;
  default?: string | number;
  values?: string[];
  min?: number;
  max?: number;
  max_length?: number;
  allowed_prefixes?: string[];
  pattern?: string;
}

export interface Command {
  id: string; slug: string; name: string; description: string; risk: Risk; template: string;
  params: Record<string, ParamDef>; timeout_s: number; max_output_kb: number;
  status: "enabled" | "disabled" | "pending"; hash: string; version: number; source: "manual" | "pack" | "proposal";
  pack_id: string; updated_at: string;
}

export interface Group {
  id: string; slug: string; name: string; description: string; member_ids: string[];
  managed_by?: string; managed_member_ids?: string[]; // groupe tenu par une synchro: verrouillé, sauf l'ajout de membres
}

export interface Identity { id: string; name: string; username: string; auth_type: "key" | "password"; public_key: string; created_at: string }

export interface SshServer {
  id: string; slug: string; name: string; host: string; port: number; identity_id: string;
  host_key_fingerprint: string; host_key_status: "unknown" | "pinned" | "mismatch";
  description: string; tags: string[]; enabled: boolean; ssh_user: string;
  managed_by?: string; ssh_user_custom?: boolean; sync_removed?: boolean; // serveur tenu par une synchro
}

export interface McpKey {
  id: string; name: string; description: string; prefix: string; expires_at: string; last_used_at: string;
  use_count: number; can_propose: boolean; disabled: boolean; rule_ids: string[]; created_at: string;
}

export interface Rule {
  id: string; name: string; server_group_ids: string[]; command_group_ids: string[]; profile: "RO" | "RW";
  write_requires_approval: boolean; enabled: boolean; key_ids: string[];
}

export interface Execution {
  id: string; key_id: string; key_name: string; actor_user_id: string; server_slug: string; command_slug: string;
  risk: Risk | ""; params: string; rendered: string | null; reason: string; rule_name: string; status: ExecStatus;
  denial_reason: string; exit_code: number | null; stdout: string; stderr: string; output_truncated: boolean;
  duration_ms: number | null; requested_at: string; started_at: string; finished_at: string;
  decided_by: string; decided_by_name: string; decided_at: string; decision_comment: string;
  command_version: number; command_hash: string; client_ip: string; mcp_session_id: string; user_agent: string; server_id: string;
}

export interface AuditEvent {
  id: string; ts: string; actor_type: "user" | "key" | "system"; actor_id: string; actor_name: string; action: string;
  target_type: string; target_id: string; target_name: string; diff: Record<string, unknown>; ip: string;
}

export interface UserAccount {
  id: string; username: string; display_name: string; role: "admin" | "approver" | "viewer"; disabled: boolean;
  theme: string; created_at: string; last_login_at: string; auth_provider: "local" | "oidc";
}

export interface OidcRoleMap { admin: string[]; approver: string[]; viewer: string[] }
export interface OidcConfig {
  enabled: boolean; name: string; issuer: string; client_id: string; has_client_secret: boolean; scopes: string; role_claim: string;
  role_map: OidcRoleMap; default_role: "" | "viewer"; jit: boolean; local_password_admins_only: boolean; end_session: boolean; redirect_url: string;
}
export interface OidcTestResult {
  ok: boolean; error?: string; issuer: string; authorization_endpoint?: string; token_endpoint?: string; jwks_uri?: string; jwks_keys?: number;
  userinfo_endpoint?: string; end_session_endpoint?: string; warnings?: string[];
}
export interface AuthProviders { oidc: { name: string; start_url: string } | null; local_password_admins_only: boolean }

export interface ChainResult { checked: number; ok: boolean; bad_seq?: number; bad_id?: string; reason?: string }

export type ExecStatus = "denied" | "pending_approval" | "rejected" | "expired" | "running" | "succeeded" | "failed" | "timeout" | "error";

export interface LintIssue { code: string; message: string; param?: string }
export interface LintResult { errors: LintIssue[]; warnings: LintIssue[] }

export interface Cell { server: string; command: string; allowed: boolean; requires_approval: boolean; rule?: string; reason?: string }

export interface PackMeta { id: string; name: string; version: string; description: string; category?: string }
export interface BundledPack extends PackMeta { command_count: number; installed_version: string; risks?: Record<"read" | "write" | "destructive", number>; command_ids?: string[] }
export interface InstalledPack extends PackMeta { installed_at: string; command_count: number }
export interface PackChange { field: string; before: string; after: string }
export interface PackItem { slug: string; name: string; risk: Risk; status: "new" | "modified" | "unchanged" | "invalid"; changes?: PackChange[]; lint: LintResult; overrides_manual?: boolean }
export interface PackPreview { pack: PackMeta; installed_version: string; items: PackItem[]; counts: Record<string, number>; warnings: number; errors: number }

export interface ProposalPayload { slug: string; name: string; description: string; template: string; params: Record<string, ParamDef>; risk: Risk }
export interface Proposal {
  id: string; key_id: string; key_name: string; payload: ProposalPayload; justification: string; lint: LintResult;
  status: "pending" | "approved" | "rejected" | "invalid"; reviewed_by: string; reviewed_at: string; review_comment: string; command_id: string; created_at: string;
}

export interface WebhookConfig { url: string; enabled: boolean; has_secret: boolean }
export interface WebhookDelivery { id: string; ts: string; event: string; ok: boolean; attempts: number; http_status: number; error: string; url: string }

export interface SyslogConfig {
  enabled: boolean; protocol: "udp" | "tcp" | "tls"; host: string; port: number; message_format: "text" | "json" | "cef"; framing: "lf" | "octet";
  facility: string; app_name: string; hostname: string; min_severity: string; types: string[];
  tls_ca_pem: string; tls_server_name: string; tls_skip_verify: boolean; tls_client_cert_pem: string; has_tls_client_key: boolean;
}
export interface SyslogStatus { state: "disabled" | "idle" | "connected" | "retrying"; queued: number; sent: number; dropped: number; last_error: string; last_error_at: string; last_success_at: string }
export interface SyslogResponse { config: SyslogConfig; status: SyslogStatus; options: { facilities: string[]; severities: string[]; types: string[] } }

export interface DashboardData {
  checklist: { id: "identity" | "server" | "pack" | "rule" | "key" | "agent"; done: boolean; href: string }[];
  stats: {
    executions_24h: Record<"succeeded" | "failed" | "denied" | "pending" | "other", number>;
    active_agents_24h: number; pending_approvals: number; pending_proposals: number;
    servers_in_anomaly: { slug: string; name: string; reason: "host_key_mismatch" | "host_key_unknown" | "ssh_unreachable" }[];
  };
  recent: Execution[];
}

export interface ProvField { key: string; kind: "text" | "number" | "bool" | "password"; label: string; help?: string; required: boolean; default?: unknown }
export interface ProvType { id: string; label: string; fields: ProvField[] }
export interface Provisioner {
  id: string; name: string; type: string; enabled: boolean; interval_min: number; identity_id: string; default_user: string;
  config: Record<string, unknown>; has_secret: boolean; rule_ids: string[];
  last_sync_at: string; last_sync_status: "" | "ok" | "error"; last_sync_message: string; created_at: string;
}
export interface SyncChange { kind: "server" | "group" | "membership"; action: string; name: string; detail?: string }
export interface SyncReport { changes: SyncChange[]; machines: number; blocked?: string }

export interface HourBucket { hour: string; ok: number; ko: number; denied: number; other: number }
export interface TopItem { name: string; count: number; failed: number; avg_ms: number; servers?: number }
export interface Insights {
  days: number; requests: number; succeeded: number; failed: number; denied: number; executed: number;
  avg_ms: number; p50_ms: number; p95_ms: number; max_ms: number; approvals: number; avg_approval_seconds: number;
  hourly: HourBucket[]; top_commands: TopItem[]; top_servers: TopItem[]; top_keys: TopItem[]; slowest_commands: TopItem[]; denial_reasons: TopItem[];
  risk: Record<"read" | "write" | "destructive", number>;
}

export interface AuditReportMeta {
  id: string; created_at: string; created_by: string; created_by_name: string; period_from: string; period_to: string;
  leash_version: string; schema_version: number; sha256: string; findings_high: number; findings_medium: number; findings_low: number;
  summary_model: string; summary_at: string; has_summary: boolean;
}
export interface AuditAIConfig { enabled: boolean; url: string; model: string; has_key: boolean }
export interface VerifyResult { digest_ok: boolean; tag_ok: boolean; digest: string; report_id: string; message: string }
