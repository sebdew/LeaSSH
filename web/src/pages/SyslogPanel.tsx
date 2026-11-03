import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, ErrorBox, Field } from "../components/ui";
import { Badge, Check2, Select, Textarea, when, type Tone } from "../components/kit";
import { toast } from "../components/Toaster";
import { api } from "../lib/api";
import { errorMessage, useApiMutation } from "../lib/queries";
import type { SyslogConfig, SyslogResponse, SyslogStatus } from "../lib/types";

const stateTone: Record<SyslogStatus["state"], Tone> = { disabled: "muted", idle: "info", connected: "read", retrying: "destructive" };
const KEY = "/settings/syslog";

export function SyslogPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: [KEY], queryFn: () => api<SyslogResponse>(KEY), refetchInterval: 5000 });
  if (!q.data) return <p className="text-muted">{t("errors.loading")}</p>;
  const { config, status, options } = q.data;
  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <SyslogForm key={JSON.stringify(config)} config={config} options={options} onChanged={() => void qc.invalidateQueries({ queryKey: [KEY] })} />
      <StatusCard status={status} />
    </div>
  );
}

function StatusCard({ status }: { status: SyslogStatus }) {
  const { t } = useTranslation();
  return (
    <Card className="flex h-fit flex-col gap-3" >
      <h2 className="text-[15px] font-semibold">{t("syslog.status")}</h2>
      <div aria-live="polite"><Badge tone={stateTone[status.state]}>{t(`syslog.state.${status.state}`)}</Badge></div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
        <dt className="text-muted">{t("syslog.queued")}</dt><dd>{status.queued}</dd>
        <dt className="text-muted">{t("syslog.sent")}</dt><dd>{status.sent}</dd>
        <dt className="text-muted">{t("syslog.dropped")}</dt><dd>{status.dropped}</dd>
        <dt className="text-muted">{t("syslog.lastSuccess")}</dt><dd>{status.last_success_at ? when(status.last_success_at) : "—"}</dd>
      </dl>
      {status.last_error && (
        <div>
          <p className="font-medium">{t("syslog.lastError")}{status.last_error_at ? ` (${when(status.last_error_at)})` : ""}</p>
          <p className="break-words text-destructive">{status.last_error}</p>
        </div>
      )}
      <p className="text-muted">{t("syslog.queueHelp")}</p>
    </Card>
  );
}

type TestResult = { ok: boolean; error?: string };

function SyslogForm({ config, options, onChanged }: { config: SyslogConfig; options: SyslogResponse["options"]; onChanged: () => void }) {
  const { t } = useTranslation();
  const [c, setC] = useState<SyslogConfig>(config);
  const [keyPem, setKeyPem] = useState("");
  const [clearKey, setClearKey] = useState(false);
  const [test, setTest] = useState<TestResult | null>(null);
  const set = <K extends keyof SyslogConfig>(k: K, v: SyslogConfig[K]) => setC((p) => ({ ...p, [k]: v }));
  const save = useApiMutation<unknown>("PUT", KEY, []);
  const testIt = useMutation({ mutationFn: () => api<TestResult>(`${KEY}/test`, { method: "POST" }), onSuccess: (r) => { setTest(r); onChanged(); } });
  const dirty = JSON.stringify(c) !== JSON.stringify(config) || keyPem !== "" || clearKey;
  const tls = c.protocol === "tls";
  return (
    <Card className="flex flex-col gap-3">
      <h2 className="text-[15px] font-semibold">{t("settings.syslog")}</h2>
      <p className="text-muted">{t("syslog.help")}</p>
      <Check2 id="sl-enabled" label={t("common.enabled")} checked={c.enabled} onChange={(v) => set("enabled", v)} />
      <div className="grid gap-3 sm:grid-cols-3">
        <Select id="sl-proto" label={t("syslog.protocol")} value={c.protocol} onChange={(e) => { set("protocol", e.target.value as SyslogConfig["protocol"]); if (e.target.value === "tls" && c.port === 514) set("port", 6514); if (e.target.value !== "tls" && c.port === 6514) set("port", 514); }}>
          <option value="udp">UDP</option><option value="tcp">TCP</option><option value="tls">TCP + TLS</option>
        </Select>
        <div className="sm:col-span-1"><Field id="sl-host" label={t("syslog.host")} value={c.host} placeholder="wazuh.example.com" onChange={(e) => set("host", e.target.value)} /></div>
        <Field id="sl-port" type="number" min={1} max={65535} label={t("syslog.port")} value={c.port} onChange={(e) => set("port", Number(e.target.value))} />
      </div>
      {c.protocol === "udp" && <p className="text-muted">{t("syslog.udpHelp")}</p>}
      <div className="grid gap-3 sm:grid-cols-2">
        <Select id="sl-format" label={t("syslog.format")} value={c.message_format} onChange={(e) => set("message_format", e.target.value as SyslogConfig["message_format"])}>
          <option value="text">{t("syslog.formats.text")}</option><option value="json">{t("syslog.formats.json")}</option><option value="cef">{t("syslog.formats.cef")}</option>
        </Select>
        {c.protocol !== "udp" && (
          <Select id="sl-framing" label={t("syslog.framing")} value={c.framing} onChange={(e) => set("framing", e.target.value as SyslogConfig["framing"])}>
            <option value="lf">{t("syslog.framings.lf")}</option><option value="octet">{t("syslog.framings.octet")}</option>
          </Select>
        )}
        <Select id="sl-facility" label={t("syslog.facility")} value={c.facility} onChange={(e) => set("facility", e.target.value)}>
          {options.facilities.map((f) => <option key={f} value={f}>{f}</option>)}
        </Select>
        <Field id="sl-app" label={t("syslog.appName")} value={c.app_name} onChange={(e) => set("app_name", e.target.value)} />
        <Field id="sl-hostname" label={t("syslog.hostname")} value={c.hostname} placeholder={t("syslog.hostnameAuto")} onChange={(e) => set("hostname", e.target.value)} />
        <Select id="sl-sev" label={t("syslog.minSeverity")} value={c.min_severity} onChange={(e) => set("min_severity", e.target.value)}>
          {options.severities.map((s) => <option key={s} value={s}>{t(`syslog.sev.${s}`)}</option>)}
        </Select>
      </div>
      <fieldset className="flex flex-col gap-2">
        <legend className="font-medium">{t("syslog.types")}</legend>
        {options.types.map((ty) => (
          <Check2 key={ty} id={`sl-type-${ty}`} label={t(`syslog.typeLabels.${ty}`)} checked={c.types.includes(ty)}
            onChange={(v) => set("types", v ? [...c.types, ty] : c.types.filter((x) => x !== ty))} />
        ))}
        <p className="text-muted">{t("syslog.typesHelp")}</p>
      </fieldset>
      {tls && (
        <fieldset className="flex flex-col gap-3 rounded-field border border-border p-3">
          <legend className="px-1 font-medium">{t("syslog.tls")}</legend>
          <Textarea id="sl-ca" mono label={t("syslog.ca")} hint={t("syslog.caHelp")} value={c.tls_ca_pem} placeholder="-----BEGIN CERTIFICATE-----" onChange={(e) => set("tls_ca_pem", e.target.value)} />
          <Field id="sl-sni" label={t("syslog.serverName")} value={c.tls_server_name} placeholder={c.host} onChange={(e) => set("tls_server_name", e.target.value)} />
          <Check2 id="sl-skip" label={t("syslog.skipVerify")} checked={c.tls_skip_verify} onChange={(v) => set("tls_skip_verify", v)} />
          {c.tls_skip_verify && <p role="alert" className="text-write">{t("syslog.skipVerifyWarn")}</p>}
          <Textarea id="sl-cert" mono label={t("syslog.clientCert")} hint={t("syslog.clientHelp")} value={c.tls_client_cert_pem} placeholder="-----BEGIN CERTIFICATE-----" onChange={(e) => set("tls_client_cert_pem", e.target.value)} />
          <Textarea id="sl-key" mono label={t("syslog.clientKey")} value={keyPem} placeholder={c.has_tls_client_key ? t("syslog.keyKept") : "-----BEGIN PRIVATE KEY-----"}
            autoComplete="off" spellCheck={false} onChange={(e) => { setKeyPem(e.target.value); setClearKey(false); }} />
          {c.has_tls_client_key && <Check2 id="sl-clearkey" label={t("syslog.clearKey")} checked={clearKey} onChange={(v) => { setClearKey(v); if (v) setKeyPem(""); }} />}
        </fieldset>
      )}
      {dirty && <p className="text-muted">{t("syslog.saveBeforeTest")}</p>}
      {save.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} />}
      {testIt.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(testIt.error)} title={t("errors.details")} />}
      {test && (test.ok ? <p className="text-read" aria-live="polite">✔ {t("syslog.testOk")}</p> : <p role="alert" className="text-destructive">{t("syslog.testFailed", { error: test.error })}</p>)}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="secondary" disabled={testIt.isPending || dirty || !config.host} onClick={() => testIt.mutate()}>{t("syslog.test")}</Button>
        <Button disabled={save.isPending || !dirty}
          onClick={() => save.mutate({ ...c, tls_client_key_pem: keyPem, clear_tls_client_key: clearKey }, { onSuccess: () => { setKeyPem(""); setClearKey(false); setTest(null); toast(t("common.saved")); onChanged(); } })}>{t("common.save")}</Button>
      </div>
    </Card>
  );
}
