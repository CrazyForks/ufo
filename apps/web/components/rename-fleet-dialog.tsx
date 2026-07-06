"use client";

import { useEffect, useState, type ReactNode } from "react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useT } from "@/lib/i18n";

export function RenameFleetDialog({ fleetId, name: initialName, trigger }: { fleetId: string; name: string; trigger: ReactNode }) {
  const app = useApp();
  const t = useT();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(initialName);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (open) setName(initialName);
  }, [initialName, open]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const next = name.trim();
    if (!next || next === initialName) return setOpen(false);
    setSaving(true);
    const ok = await app.updateFleet(fleetId, next);
    setSaving(false);
    if (ok) setOpen(false);
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("fleet.renameTitle")}</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-3">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t("fleet.newName")} autoFocus />
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>{t("common.cancel")}</Button>
            <Button type="submit" disabled={saving}>{saving ? t("common.saving") : t("common.save")}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
