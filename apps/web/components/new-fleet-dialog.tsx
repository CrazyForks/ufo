"use client";

import { useState, type ReactNode } from "react";
import { Plus } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useT } from "@/lib/i18n";

export function NewFleetDialog({ trigger }: { trigger?: ReactNode }) {
  const app = useApp();
  const t = useT();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [context, setContext] = useState("");
  const [creating, setCreating] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setCreating(true);
    await app.createFleet(name.trim(), context);
    setCreating(false);
    setName("");
    setContext("");
    setOpen(false);
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        {trigger ?? <Button variant="ghost" size="icon-sm" title={t("fleet.newTitle")}><Plus /></Button>}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("fleet.newTitle")}</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-3">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("fleet.newName")} autoFocus />
          <Textarea value={context} onChange={(e) => setContext(e.target.value)} placeholder={t("fleet.contextPlaceholder")} className="min-h-20 resize-y text-sm" />
          <p className="text-xs text-muted-foreground">{t("fleet.hint")}</p>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>{t("common.cancel")}</Button>
            <Button type="submit" disabled={creating}>{creating ? t("auth.creating") : t("common.create")}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
