import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, ErrorBox, Field } from "../components/ui";
import { Check2, Select, Textarea } from "../components/kit";
import { toast } from "../components/Toaster";
import { api } from "../lib/api";
import { errorMessage, useApiMutation } from "../lib/queries";
import type { GeneralSettings } from "../lib/types";

const KEY = "/settings/general";

export function GeneralPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: [KEY], queryFn: () => api<GeneralSettings>(KEY) });
  if (!q.data) return <p className="text-muted">{t("errors.loading")}</p>;
  return <GeneralForm key={JSON.stringify(q.data)} config={q.data} onSaved={() => { void qc.invalidateQueries({ queryKey: [KEY] }); void qc.invalidateQueries({ queryKey: ["meta"] }); }} />;
}

function GeneralForm({ config, onSaved }: { config: GeneralSettings; onSaved: () => void }) {
  const { t } = useTranslation();
  const [c, setC] = useState(config);
  const [token, setToken] = useState("");
  const [clearToken, setClearToken] = useState(false);
  const set = <K extends keyof GeneralSettings>(k: K, v: GeneralSettings[K]) => setC((p) => ({ ...p, [k]: v }));
  const num = (k: "session_ttl_hours" | "approval_ttl_minutes" | "log_retention_days") => (e: React.ChangeEvent<HTMLInputElement>) => set(k, Number(e.target.value));
  const save = useApiMutation<unknown>("PUT", KEY, []);
  const dirty = JSON.stringify(c) !== JSON.stringify(config) || token !== "" || clearToken;
  const submit = () => {
    const { has_metrics_token: _h, detected_url: _d, ...body } = c;
    save.mutate({ ...body, metrics_token: token, clear_metrics_token: clearToken }, { onSuccess: () => { setToken(""); setClearToken(false); toast(t("common.saved")); onSaved(); } });
  };
  return (
    <div className="grid max-w-3xl gap-4">
      <Card className="flex flex-col gap-3">
        <h2 className="text-[15px] font-semibold">{t("general.network")}</h2>
        <Field id="g-base" label={t("general.baseUrl")} value={c.base_url} placeholder={config.detected_url} autoComplete="off" onChange={(e) => set("base_url", e.target.value)} />
        <p className="text-muted">{t("general.baseUrlHelp", { url: config.detected_url })}</p>
        <Textarea id="g-proxies" mono label={t("general.proxies")} value={c.trusted_proxies} rows={2} spellCheck={false} placeholder="10.0.0.0/8, 192.168.1.5" onChange={(e) => set("trusted_proxies", e.target.value)} />
        <p className="text-muted">{t("general.proxiesHelp")}</p>
      </Card>
      <Card className="flex flex-col gap-3">
        <h2 className="text-[15px] font-semibold">{t("general.behaviour")}</h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field id="g-session" type="number" min={1} label={t("general.sessionTtl")} value={String(c.session_ttl_hours)} onChange={num("session_ttl_hours")} />
          <Field id="g-approval" type="number" min={1} label={t("general.approvalTtl")} value={String(c.approval_ttl_minutes)} onChange={num("approval_ttl_minutes")} />
          <Field id="g-retention" type="number" min={0} label={t("general.retention")} value={String(c.log_retention_days)} onChange={num("log_retention_days")} />
        </div>
        <p className="text-muted">{t("general.behaviourHelp")}</p>
        <Select id="g-level" label={t("general.logLevel")} value={c.log_level} onChange={(e) => set("log_level", e.target.value as GeneralSettings["log_level"])}>
          {["debug", "info", "warn", "error"].map((l) => <option key={l} value={l}>{l}</option>)}
        </Select>
      </Card>
      <Card className="flex flex-col gap-3">
        <h2 className="text-[15px] font-semibold">{t("general.metrics")}</h2>
        <Field id="g-metrics" type="password" label={t("general.metricsToken")} value={token} autoComplete="new-password" spellCheck={false}
          placeholder={config.has_metrics_token ? t("general.metricsKept") : t("general.metricsOpen")} onChange={(e) => { setToken(e.target.value); setClearToken(false); }} />
        <p className="text-muted">{t("general.metricsHelp")}</p>
        {config.has_metrics_token && <Check2 id="g-metrics-clear" label={t("general.metricsClear")} checked={clearToken} onChange={(v) => { setClearToken(v); if (v) setToken(""); }} />}
      </Card>
      {save.error && <ErrorBox message={t("errors.generic")} detail={errorMessage(save.error)} title={t("errors.details")} />}
      <div className="flex justify-end"><Button disabled={save.isPending || !dirty} onClick={submit}>{t("common.save")}</Button></div>
    </div>
  );
}
