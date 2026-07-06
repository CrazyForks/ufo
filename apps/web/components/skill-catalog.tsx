"use client";

import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Archive, BookOpen, FileText, Plus } from "lucide-react";
import { toast } from "sonner";
import { del, getJSON, postJSON, withFleet } from "@/lib/api";
import type { Skill } from "@/lib/types";
import { useT } from "@/lib/i18n";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

async function responseError(res: Response, fallback: string) {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error || fallback;
  } catch {
    return fallback;
  }
}

export function SkillCatalog({ fleetId, canManage }: { fleetId: string; canManage: boolean }) {
  const t = useT();
  const [skills, setSkills] = useState<Skill[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [formError, setFormError] = useState("");
  const [saving, setSaving] = useState(false);
  const [archiving, setArchiving] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [description, setDescription] = useState("");
  const [content, setContent] = useState("");

  const loadSkills = useCallback(async () => {
    if (!canManage) {
      setSkills([]);
      setLoadError(false);
      return;
    }
    setLoading(true);
    setLoadError(false);
    try {
      const rows = await getJSON<Skill[]>(withFleet("/api/v1/skills", fleetId));
      if (rows) setSkills(rows);
      else setLoadError(true);
    } catch {
      setLoadError(true);
    }
    setLoading(false);
  }, [canManage, fleetId]);

  useEffect(() => {
    void loadSkills();
  }, [loadSkills]);

  useEffect(() => {
    setName("");
    setSlug("");
    setDescription("");
    setContent("");
    setFormError("");
  }, [fleetId]);

  async function createSkill(e: FormEvent) {
    e.preventDefault();
    const title = name.trim();
    if (!title || !content.trim()) return;
    setSaving(true);
    setFormError("");
    const body = {
      fleet_id: fleetId,
      name: title,
      slug: slug.trim() || undefined,
      description: description.trim(),
      files: [{ path: "SKILL.md", content }],
    };
    const res = await postJSON("/api/v1/skills", body);
    if (!res.ok) {
      const message = await responseError(res, t("skills.saveError"));
      setFormError(message);
      toast.error(message);
      setSaving(false);
      return;
    }
    const skill = (await res.json()) as Skill;
    setSkills((prev) => [skill, ...prev.filter((it) => it.id !== skill.id)].sort((a, b) => a.name.localeCompare(b.name)));
    setName("");
    setSlug("");
    setDescription("");
    setContent("");
    setSaving(false);
    toast.success(t("toast.skillSaved"));
  }

  async function archiveSkill(skill: Skill) {
    if (!window.confirm(t("skills.archiveConfirm", { name: skill.name }))) return;
    setArchiving(skill.id);
    setFormError("");
    const res = await del(`/api/v1/skills/${skill.id}`);
    if (!res.ok) {
      const message = await responseError(res, t("skills.archiveError"));
      setFormError(message);
      toast.error(message);
      setArchiving(null);
      return;
    }
    setSkills((prev) => prev.filter((it) => it.id !== skill.id));
    setArchiving(null);
    toast.success(t("toast.skillArchived"));
  }

  const canCreate = canManage && !!name.trim() && !!content.trim() && !saving;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base"><BookOpen className="size-4" /> {t("settings.skills")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        {!canManage ? (
          <p className="rounded-md border border-border px-3 py-2 text-xs text-muted-foreground">{t("skills.adminsOnly")}</p>
        ) : (
          <>
            <form onSubmit={createSkill} className="space-y-3">
              <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_12rem]">
                <Label className="block space-y-1">
                  <span className="text-xs text-muted-foreground">{t("common.name")}</span>
                  <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("skills.namePlaceholder")} className="h-8" />
                </Label>
                <Label className="block space-y-1">
                  <span className="text-xs text-muted-foreground">{t("skills.slug")}</span>
                  <Input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder={t("skills.slugPlaceholder")} className="h-8 font-mono" />
                </Label>
              </div>
              <Label className="block space-y-1">
                <span className="text-xs text-muted-foreground">{t("common.description")}</span>
                <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder={t("skills.descriptionPlaceholder")} className="h-8" />
              </Label>
              <Label className="block space-y-1">
                <span className="text-xs text-muted-foreground">SKILL.md</span>
                <Textarea value={content} onChange={(e) => setContent(e.target.value)} placeholder={t("skills.contentPlaceholder")} className="min-h-36 resize-y font-mono text-xs" />
              </Label>
              <div className="flex items-center justify-between gap-3">
                <p className="min-w-0 text-xs text-destructive">{formError}</p>
                <Button type="submit" size="sm" disabled={!canCreate}>
                  <Plus /> {saving ? t("common.saving") : t("skills.create")}
                </Button>
              </div>
            </form>
            <div className="space-y-2 border-t border-border pt-3">
              {loading && <p className="text-sm text-muted-foreground">{t("common.loading")}</p>}
              {!loading && loadError && <p className="text-sm text-destructive">{t("skills.loadError")}</p>}
              {!loading && !loadError && skills.length === 0 && <p className="text-sm text-muted-foreground">{t("skills.empty")}</p>}
              {!loading && !loadError && skills.map((skill) => (
                <div key={skill.id} className="flex items-start gap-3 rounded-md border border-border px-3 py-2">
                  <FileText className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                      <span className="truncate font-medium">{skill.name}</span>
                      <Badge variant="secondary" className="font-mono">{skill.slug}</Badge>
                      <Badge variant="outline">
                        {skill.files.length === 1 ? t("skills.fileCountOne") : t("skills.fileCount", { count: skill.files.length })}
                      </Badge>
                    </div>
                    {skill.description && <p className="line-clamp-2 text-xs text-muted-foreground">{skill.description}</p>}
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    className="shrink-0 text-muted-foreground hover:text-destructive"
                    title={t("common.archive")}
                    aria-label={t("skills.archiveLabel", { name: skill.name })}
                    disabled={archiving === skill.id}
                    onClick={() => void archiveSkill(skill)}
                  >
                    <Archive />
                  </Button>
                </div>
              ))}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
