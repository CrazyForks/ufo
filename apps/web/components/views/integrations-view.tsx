"use client";

import { useState, type FormEvent } from "react";
import { GitPullRequest, Plus, Trash2 } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useT } from "@/lib/i18n";
import type { Forge } from "@/lib/types";

export function IntegrationsView() {
  const app = useApp();
  const t = useT();
  const canEdit = app.myRole === "owner" || app.myRole === "admin";

  return (
    <div className="h-full min-h-0 overflow-y-auto">
      <div className="mx-auto max-w-3xl space-y-3 p-4 pb-8">
        <Card>
          <CardHeader className="px-4 py-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <GitPullRequest className="size-4" /> {t("integrations.forge")}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2.5 px-4 pb-4 pt-0 text-sm">
            <p className="text-[11px] leading-snug text-muted-foreground">{t("integrations.forgeHint")}</p>
            <ForgeCatalog canEdit={canEdit} />
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function ForgeCatalog({ canEdit }: { canEdit: boolean }) {
  const app = useApp();
  const t = useT();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const [provider, setProvider] = useState<"github" | "gitlab">("github");
  const [repo, setRepo] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [defaultBase, setDefaultBase] = useState("main");
  const [credName, setCredName] = useState("UFO_ROVER_FORGE_TOKEN");
  const [saving, setSaving] = useState(false);

  function resetForm() {
    setEditingId(null);
    setKey("");
    setName("");
    setProvider("github");
    setRepo("");
    setBaseUrl("");
    setDefaultBase("main");
    setCredName("UFO_ROVER_FORGE_TOKEN");
  }

  function editRow(row: Forge) {
    setEditingId(row.id);
    setKey(row.key);
    setName(row.name);
    setProvider(row.provider === "gitlab" ? "gitlab" : "github");
    setRepo(row.repo);
    setBaseUrl(row.base_url);
    setDefaultBase(row.default_base_branch || "main");
    const cred = row.credential ?? {};
    setCredName(typeof cred.name === "string" && cred.name.trim() ? cred.name.trim() : "UFO_ROVER_FORGE_TOKEN");
  }

  async function save(e: FormEvent) {
    e.preventDefault();
    if (!canEdit || !key.trim() || !repo.trim()) return;
    setSaving(true);
    try {
      const input = {
        key: key.trim(),
        name: name.trim(),
        provider,
        repo: repo.trim(),
        base_url: baseUrl.trim() || undefined,
        default_base_branch: defaultBase.trim() || "main",
        credential_kind: "rover_env" as const,
        credential: { name: credName.trim() || "UFO_ROVER_FORGE_TOKEN" },
      };
      if (editingId) await app.updateForge(editingId, input);
      else await app.createForge(input);
      resetForm();
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-3">
      {app.forges.length === 0 ? (
        <p className="text-[11px] text-muted-foreground">{t("integrations.forgeEmpty")}</p>
      ) : (
        <ul className="divide-y divide-border rounded-md border border-border">
          {app.forges.map((row) => (
            <li key={row.id} className="flex items-start justify-between gap-2 px-2.5 py-2">
              <button type="button" className="min-w-0 flex-1 text-left" onClick={() => canEdit && editRow(row)} disabled={!canEdit}>
                <div className="truncate font-mono text-xs font-medium">{row.key}</div>
                <div className="truncate text-[11px] text-muted-foreground">
                  {row.provider} · {row.repo}
                  {row.name ? ` · ${row.name}` : ""}
                </div>
                <div className="truncate text-[10px] text-muted-foreground">{row.credential_kind}</div>
              </button>
              {canEdit ? (
                <Button type="button" variant="ghost" size="icon-sm" aria-label={t("common.delete")} onClick={() => app.deleteForge(row.id)}>
                  <Trash2 />
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      )}

      {canEdit ? (
        <form onSubmit={save} className="space-y-2 rounded-md border border-border p-2.5">
          <div className="text-[11px] font-medium text-muted-foreground">
            {editingId ? t("integrations.forgeEdit") : t("integrations.forgeAdd")}
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div className="space-y-1">
              <Label className="text-[11px] text-muted-foreground">{t("integrations.forgeKey")}</Label>
              <Input value={key} onChange={(e) => setKey(e.target.value.toLowerCase())} placeholder="ufo-core" className="h-8 font-mono text-xs" />
            </div>
            <div className="space-y-1">
              <Label className="text-[11px] text-muted-foreground">{t("common.name")}</Label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("integrations.forgeNamePlaceholder")} className="h-8 text-xs" />
            </div>
          </div>
          <div className="space-y-1">
            <Label className="text-[11px] text-muted-foreground">{t("integrations.forgeProvider")}</Label>
            <Select value={provider} onValueChange={(v) => setProvider(v as "github" | "gitlab")}>
              <SelectTrigger className="h-8"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="github">GitHub</SelectItem>
                <SelectItem value="gitlab">GitLab</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label className="text-[11px] text-muted-foreground">{t("integrations.forgeRepo")}</Label>
            <Input value={repo} onChange={(e) => setRepo(e.target.value)} placeholder={t("integrations.forgeRepoPlaceholder")} className="h-8 font-mono text-xs" />
          </div>
          <div className="space-y-1">
            <Label className="text-[11px] text-muted-foreground">{t("integrations.forgeBaseUrl")}</Label>
            <Input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder={provider === "gitlab" ? "https://gitlab.com/api/v4" : "https://api.github.com"} className="h-8 font-mono text-xs" />
          </div>
          <div className="space-y-1">
            <Label className="text-[11px] text-muted-foreground">{t("integrations.forgeDefaultBase")}</Label>
            <Input value={defaultBase} onChange={(e) => setDefaultBase(e.target.value)} placeholder="main" className="h-8 font-mono text-xs" />
          </div>
          <div className="space-y-1">
            <Label className="text-[11px] text-muted-foreground">{t("integrations.forgeCredential")}</Label>
            <Input
              value={credName}
              onChange={(e) => setCredName(e.target.value)}
              placeholder="UFO_ROVER_FORGE_TOKEN"
              className="h-8 font-mono text-xs"
            />
            <p className="text-[11px] text-muted-foreground">{t("integrations.forgeCredentialHint")}</p>
          </div>
          <div className="flex justify-end gap-1.5">
            {editingId ? (
              <Button type="button" variant="ghost" size="sm" onClick={resetForm}>{t("common.cancel")}</Button>
            ) : null}
            <Button type="submit" variant="outline" size="sm" disabled={saving || !key.trim() || !repo.trim()}>
              <Plus /> {saving ? t("common.saving") : editingId ? t("common.save") : t("integrations.forgeAdd")}
            </Button>
          </div>
        </form>
      ) : null}
    </div>
  );
}
