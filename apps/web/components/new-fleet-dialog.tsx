"use client";

import { useState, type ReactNode } from "react";
import { Plus } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export function NewFleetDialog({ trigger }: { trigger?: ReactNode }) {
  const app = useApp();
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
        {trigger ?? <Button variant="ghost" size="icon-sm" title="New fleet"><Plus /></Button>}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New fleet</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-3">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Fleet name" autoFocus />
          <Textarea value={context} onChange={(e) => setContext(e.target.value)} placeholder="Context (root repo, default branch, constraints...)" className="min-h-20 resize-y text-sm" />
          <p className="text-xs text-muted-foreground">A group fleet you can invite teammates to and manage.</p>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" disabled={creating}>{creating ? "Creating…" : "Create"}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
