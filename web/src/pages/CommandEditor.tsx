import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Play, Save } from "lucide-react";
import { Button, Card, ErrorBox, Field } from "../components/ui";
import { Badge, Check2, CodeBlock, MultiPick, PageHeader, RiskBadge, Select, Textarea } from "../components/kit";
import { toast } from "../components/Toaster";
import { api, ApiError } from "../lib/api";
import { errorMessage, useList } from "../lib/queries";
import type { Command, Execution, Group, LintResult, McpKey, ParamDef, ParamType, Proposal, Risk, Rule, SshServer } from "../lib/types";

const PARAM_TYPES: ParamType[] = ["enum", "int", "identifier", "service", "path", "regex"];
const RISKS: Risk[] = ["read", "write", "destructive"];

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => { const h = setTimeout(() => setV(value), ms); return () => clearTimeout(h); }, [value, ms]);
  return v;
}

// Template avec placeholders colorés (jetons de couleur uniquement).
export function Highlighted({ template }: { template: string }) {
  const parts = template.split(/(\{\{[a-z][a-z0-9_]*\}\})/g);
  return (
    <code className="whitespace-pre-wrap break-all font-mono text-[13px]" data-testid="highlighted">
      {parts.map((p, i) => (p.startsWith("{{") ? <span key={i} className="rounded bg-primary/15 px-0.5 text-primary">{p}</span> : <span key={i}>{p}</span>))}
    </code>
  );
}

// Mode proposition (« Modifier puis approuver »): même éditeur, linter strict, risque définitif et groupes au moment d'approuver.
const NO_LINT: LintResult = { errors: [], warnings: [] };
const NO_NAMES: string[] = [];
const NO_PREVIEW = { ok: false } as { ok: boolean; rendered?: string; error?: string };

export function CommandEditor({ id, proposal }: { id?: string; proposal?: Proposal }) {
  const { t } = useTranslation();
  const nav = useNavigate();
  const qc = useQueryClient();
  const existing = useList<Command>("/commands");
  const cgroups = useList<Group>("/command-groups");
  const current = useMemo(() => existing.data?.find((c) => c.id === id), [existing.data, id]);

  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [risk, setRisk] = useState<Risk>("read");
  const [template, setTemplate] = useState("");
  const [params, setParams] = useState<Record<string, ParamDef>>({});
  const [timeoutS, setTimeoutS] = useState(30);
  const [maxKB, setMaxKB] = useState(64);
  const [enabled, setEnabled] = useState(true);
  const [loaded, setLoaded] = useState(!id && !proposal);
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [comment, setComment] = useState("");
  const [saveError, setSaveError] = useState<{ message: string; detail?: string } | null>(null);
  const [saving, setSaving] = useState(false);

  // chargement initial du formulaire, une seule fois, pendant le rendu (pas d'effet : https://react.dev/learn/you-might-not-need-an-effect)
  if (!loaded && proposal) {
    const p = proposal.payload;
    setSlug(p.slug); setName(p.name); setDescription(p.description); setRisk(p.risk); setTemplate(p.template); setParams(p.params ?? {});
    setLoaded(true);
  } else if (!loaded && current) {
    setSlug(current.slug); setName(current.name); setDescription(current.description); setRisk(current.risk);
    setTemplate(current.template); setParams(current.params); setTimeoutS(current.timeout_s); setMaxKB(current.max_output_kb);
    setEnabled(current.status === "enabled"); setLoaded(true);
  }

  // linter en direct + détection des placeholders
  const [lintRes, setLintRes] = useState<{ lint: LintResult; placeholders: string[] } | null>(null);
  const dTemplate = useDebounced(template, 250);
  const dParams = useDebounced(params, 250);
  const lintActive = loaded && !!dTemplate;
  const lint = lintActive && lintRes ? lintRes.lint : NO_LINT;
  const placeholders = lintActive && lintRes ? lintRes.placeholders : NO_NAMES;
  useEffect(() => {
    if (!loaded || !dTemplate) return;
    let live = true;
    api<{ lint: LintResult; placeholders: string[] }>(proposal ? "/proposals/lint" : "/commands/lint", { method: "POST", body: proposal ? { slug, name, description, template: dTemplate, params: dParams, risk } : { template: dTemplate, params: dParams, risk } })
      .then((r) => {
        if (!live) return;
        const placeholders = r.placeholders ?? [];
        setLintRes({ lint: { errors: r.lint?.errors ?? [], warnings: r.lint?.warnings ?? [] }, placeholders });
        // paramètres détectés automatiquement depuis le template
        setParams((cur) => {
          const next: Record<string, ParamDef> = {};
          for (const n of placeholders) next[n] = cur[n] ?? { type: "identifier" };
          const same = Object.keys(next).length === Object.keys(cur).length && Object.keys(next).every((k) => cur[k] === next[k]);
          return same ? cur : next;
        });
      })
      .catch(() => undefined);
    return () => { live = false; };
  }, [dTemplate, dParams, risk, loaded, proposal, slug, name, description]);

  // aperçu avec l'échappement réel
  const [values, setValues] = useState<Record<string, string>>({});
  const dValues = useDebounced(values, 250);
  const [previewRes, setPreview] = useState<{ ok: boolean; rendered?: string; error?: string }>({ ok: false });
  const preview = dTemplate ? previewRes : NO_PREVIEW;
  useEffect(() => {
    if (!dTemplate) return;
    let live = true;
    const typed: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(dValues)) typed[k] = params[k]?.type === "int" && v !== "" && !Number.isNaN(Number(v)) ? Number(v) : v;
    api<{ ok: boolean; rendered?: string; error?: string }>("/commands/preview", { method: "POST", body: { template: dTemplate, params: dParams, risk, values: typed } })
      .then((r) => { if (live) setPreview(r); }).catch(() => undefined);
    return () => { live = false; };
  }, [dTemplate, dParams, dValues, risk, params]);

  const save = async () => {
    setSaving(true); setSaveError(null);
    try {
      if (proposal) {
        await api(`/proposals/${proposal.id}/approve`, { method: "POST", body: { edited: { slug, name, description, risk, template, params }, group_ids: groupIds, comment } });
        await qc.invalidateQueries({ queryKey: ["/proposals"] });
        await qc.invalidateQueries({ queryKey: ["/commands"] });
        toast(t("proposals.approvedToast"));
        void nav({ to: "/propositions" });
        return;
      }
      const body = { slug, name, description, risk, template, params, timeout_s: timeoutS, max_output_kb: maxKB, status: enabled ? "enabled" : "disabled" };
      const res = await api<{ command: Command }>(id ? `/commands/${id}` : "/commands", { method: id ? "PUT" : "POST", body });
      await qc.invalidateQueries({ queryKey: ["/commands"] });
      toast(t("common.saved"));
      if (!id) void nav({ to: "/commandes/$id", params: { id: res.command.id } });
    } catch (e) {
      setSaveError({ message: e instanceof ApiError ? e.message : t("errors.generic"), detail: errorMessage(e) });
    } finally { setSaving(false); }
  };

  const setParam = (name: string, patch: Partial<ParamDef>) => setParams((p) => ({ ...p, [name]: { ...p[name], ...patch } }));
  const descLen = description.length;

  return (
    <>
      <PageHeader title={proposal ? t("proposals.editApproveTitle") : id ? t("editor.editTitle", { slug }) : t("editor.newTitle")} subtitle={proposal ? t("proposals.editApproveSubtitle") : t("editor.subtitle")}
        action={<div className="flex gap-2"><Link to={proposal ? "/propositions" : "/commandes"} className="inline-flex h-9 items-center rounded-field border border-border-strong bg-surface-2 px-4">{t("common.back")}</Link>
          <Button onClick={() => void save()} disabled={saving || !slug || !name || !template || (!!proposal && lint.errors.length > 0)}><Save aria-hidden size={16} />{proposal ? t("proposals.approve") : t("common.save")}</Button></div>} />
      {saveError && <div className="mb-4"><ErrorBox {...saveError} title={t("errors.details")} /></div>}

      <div className="grid gap-4 lg:grid-cols-[1fr_380px]">
        <div className="flex flex-col gap-4">
          <Card className="flex flex-col gap-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field id="slug" label={t("common.slug")} value={slug} onChange={(e) => setSlug(e.target.value)} placeholder="systemd.status" />
              <Field id="name" label={t("common.name")} value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <Textarea id="description" label={t("common.description")} value={description} onChange={(e) => setDescription(e.target.value)}
              hint={`${t("editor.descriptionHint")} (${descLen}/4000)`} maxLength={4000} />
            <fieldset>
              <legend className="mb-1.5 font-medium">{t("editor.risk")}</legend>
              <div role="radiogroup" className="inline-flex gap-1 rounded-field border border-border p-1">
                {RISKS.map((r) => (
                  <button key={r} type="button" role="radio" aria-checked={risk === r} onClick={() => setRisk(r)}
                    className={`rounded-field px-3 py-1 ${risk === r ? "bg-surface-2 ring-2 ring-primary" : "ring-1 ring-border hover:bg-surface-2"}`}><RiskBadge risk={r} /></button>
                ))}
              </div>
            </fieldset>
            {proposal && (
              <>
                <MultiPick label={t("proposals.addToGroups")} options={(cgroups.data ?? []).map((g) => ({ id: g.id, label: g.name }))} value={groupIds} onChange={setGroupIds} />
                <Field id="review-comment" label={t("approvals.comment")} value={comment} onChange={(e) => setComment(e.target.value)} />
              </>
            )}
            <div className={`grid gap-3 sm:grid-cols-3 ${proposal ? "hidden" : ""}`}>
              <Field id="timeout" label={t("editor.timeout")} type="number" min={1} max={600} value={timeoutS} onChange={(e) => setTimeoutS(Number(e.target.value))} />
              <Field id="maxout" label={t("editor.maxOutput")} type="number" min={1} max={4096} value={maxKB} onChange={(e) => setMaxKB(Number(e.target.value))} />
              <div className="flex items-end pb-2"><Check2 id="enabled" label={t("editor.enabled")} checked={enabled} onChange={setEnabled} /></div>
            </div>
          </Card>

          <Card className="flex flex-col gap-3">
            <Textarea id="template" mono label={t("editor.template")} value={template} onChange={(e) => setTemplate(e.target.value)}
              placeholder="journalctl -u {{service}} -n {{lines}} --no-pager" spellCheck={false} hint={t("editor.templateHint")} />
            {template && <div className="rounded-field bg-surface-2 p-3"><Highlighted template={template} /></div>}
            <div aria-live="polite" className="flex flex-col gap-1">
              {lint.errors.map((i, n) => <p key={`e${n}`} role="alert" className="text-destructive">✖ {i.message}</p>)}
              {lint.warnings.map((i, n) => <p key={`w${n}`} className="text-write">▲ {i.message}</p>)}
              {template && lint.errors.length === 0 && lint.warnings.length === 0 && <p className="text-read">✔ {t("editor.lintOk")}</p>}
            </div>
          </Card>

          <Card className="flex flex-col gap-3">
            <h2 className="text-[15px] font-semibold">{t("editor.params")}</h2>
            {placeholders.length === 0 && <p className="text-muted">{t("editor.noParams")}</p>}
            {placeholders.map((n) => params[n] && <ParamRow key={n} name={n} def={params[n]} onChange={(p) => setParam(n, p)} />)}
          </Card>
        </div>

        <div className="flex flex-col gap-4">
          <Card className="flex flex-col gap-3">
            <h2 className="text-[15px] font-semibold">{t("editor.preview")}</h2>
            {placeholders.map((n) => <Field key={n} id={`pv-${n}`} label={n} value={values[n] ?? ""} onChange={(e) => setValues((v) => ({ ...v, [n]: e.target.value }))} />)}
            {preview.ok && preview.rendered ? <CodeBlock copy>{preview.rendered}</CodeBlock> : preview.error && Object.values(values).some((v) => v !== "") ? <p role="alert" className="text-destructive">{preview.error}</p> : <p className="text-muted">{t("editor.previewEmpty")}</p>}
          </Card>
          {id && !proposal && <TestPanel id={id} params={params} values={values} />}
          {id && !proposal && <UsedBy id={id} />}
        </div>
      </div>
    </>
  );
}

function ParamRow({ name, def, onChange }: { name: string; def: ParamDef; onChange: (p: Partial<ParamDef>) => void }) {
  const { t } = useTranslation();
  const csv = (v?: string[]) => (v ?? []).join(", ");
  const parse = (v: string) => v.split(",").map((s) => s.trim()).filter(Boolean);
  const num = (v: string) => (v === "" ? undefined : Number(v));
  return (
    <div className="rounded-field border border-border p-3" data-testid={`param-${name}`}>
      <div className="mb-2 flex flex-wrap items-center gap-3">
        <code className="rounded bg-primary/15 px-1 font-mono text-primary">{`{{${name}}}`}</code>
        <Select id={`pt-${name}`} label={t("editor.paramType")} value={def.type} onChange={(e) => onChange({ type: e.target.value as ParamType })}>
          {PARAM_TYPES.map((p) => <option key={p} value={p}>{p}</option>)}
        </Select>
        <Check2 id={`req-${name}`} label={t("editor.required")} checked={def.required !== false} onChange={(v) => onChange({ required: v })} />
      </div>
      <div className="grid gap-2 sm:grid-cols-2">
        <Field id={`pd-${name}`} label={t("common.description")} value={def.description ?? ""} onChange={(e) => onChange({ description: e.target.value })} />
        {def.type === "enum" && <Field id={`pv-${name}-values`} label={t("editor.enumValues")} value={csv(def.values)} onChange={(e) => onChange({ values: parse(e.target.value) })} />}
        {def.type === "int" && <>
          <Field id={`pmin-${name}`} label="min" type="number" value={def.min ?? ""} onChange={(e) => onChange({ min: num(e.target.value) })} />
          <Field id={`pmax-${name}`} label="max" type="number" value={def.max ?? ""} onChange={(e) => onChange({ max: num(e.target.value) })} />
        </>}
        {def.type === "path" && <Field id={`ppre-${name}`} label={t("editor.prefixes")} value={csv(def.allowed_prefixes)} onChange={(e) => onChange({ allowed_prefixes: parse(e.target.value) })} placeholder="/var/log, /etc/nginx" />}
        {def.type === "regex" && <>
          <Field id={`ppat-${name}`} label={t("editor.pattern")} value={def.pattern ?? ""} onChange={(e) => onChange({ pattern: e.target.value })} />
          <Field id={`pml-${name}`} label={t("editor.maxLength")} type="number" value={def.max_length ?? ""} onChange={(e) => onChange({ max_length: num(e.target.value) })} />
        </>}
        <Field id={`pdef-${name}`} label={t("editor.default")} value={def.default ?? ""} onChange={(e) => onChange({ default: e.target.value === "" ? undefined : e.target.value })} />
      </div>
    </div>
  );
}

// Test réel: exécution journalisée (actor = utilisateur), sortie affichée sous l'éditeur.
function TestPanel({ id, params, values }: { id: string; params: Record<string, ParamDef>; values: Record<string, string> }) {
  const { t } = useTranslation();
  const servers = useList<SshServer>("/servers");
  const [server, setServer] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ execution?: Execution; message?: string; error?: string } | null>(null);
  const usable = (servers.data ?? []).filter((s) => s.host_key_status === "pinned" && s.enabled);
  const run = async () => {
    setBusy(true); setResult(null);
    const typed: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(values)) if (v !== "") typed[k] = params[k]?.type === "int" ? Number(v) : v;
    try {
      setResult(await api("/commands/" + id + "/test", { method: "POST", body: { server, params: typed } }));
    } catch (e) { setResult({ error: errorMessage(e) }); } finally { setBusy(false); }
  };
  const ex = result?.execution;
  return (
    <Card className="flex flex-col gap-3">
      <h2 className="text-[15px] font-semibold">{t("editor.test")}</h2>
      <Select id="test-server" label={t("editor.testServer")} value={server} onChange={(e) => setServer(e.target.value)}>
        <option value="">—</option>
        {usable.map((s) => <option key={s.id} value={s.slug}>{s.slug}</option>)}
      </Select>
      <Button onClick={() => void run()} disabled={!server || busy}><Play aria-hidden size={16} />{t("editor.testRun")}</Button>
      {result?.error && <ErrorBox message={t("errors.generic")} detail={result.error} title={t("errors.details")} />}
      {result?.message && <p role="alert" className="text-destructive">{result.message}</p>}
      {ex && (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2"><Badge tone={ex.status === "succeeded" ? "read" : "destructive"}>{t(`status.${ex.status}`)}</Badge>
            {ex.exit_code !== null && <span className="text-muted">exit {ex.exit_code}</span>}</div>
          {ex.stdout && <pre className="max-h-64 overflow-auto rounded-field bg-surface-2 p-2 font-mono text-[12px]">{ex.stdout}</pre>}
          {ex.stderr && <pre className="max-h-40 overflow-auto rounded-field bg-surface-2 p-2 font-mono text-[12px] text-destructive">{ex.stderr}</pre>}
        </div>
      )}
    </Card>
  );
}

// « Où est-elle utilisée ? » : groupes, règles et clés qui y ont accès.
function UsedBy({ id }: { id: string }) {
  const { t } = useTranslation();
  const groups = useList<Group>("/command-groups");
  const rules = useList<Rule>("/rules");
  const keys = useList<McpKey>("/keys");
  const mine = (groups.data ?? []).filter((g) => g.member_ids.includes(id));
  const myRules = (rules.data ?? []).filter((r) => mine.some((g) => r.command_group_ids.includes(g.id)));
  const myKeys = (keys.data ?? []).filter((k) => myRules.some((r) => r.key_ids.includes(k.id)));
  return (
    <Card className="flex flex-col gap-2">
      <h2 className="text-[15px] font-semibold">{t("editor.usedBy")}</h2>
      <p><span className="text-muted">{t("editor.groups")} : </span>{mine.map((g) => g.name).join(", ") || "—"}</p>
      <p><span className="text-muted">{t("editor.rules")} : </span>{myRules.map((r) => r.name).join(", ") || "—"}</p>
      <p><span className="text-muted">{t("editor.keys")} : </span>{myKeys.map((k) => k.name).join(", ") || "—"}</p>
    </Card>
  );
}
