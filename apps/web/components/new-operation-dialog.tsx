"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { PilotIcon } from "@/components/pilot-icon";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { pilotLabel } from "@/lib/labels";

export function NewOperationDialog() {
  const app = useApp();
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [assignee, setAssignee] = useState("me");
  const [startImmediately, setStartImmediately] = useState(true);
  const [missionId, setMissionId] = useState<string>("");
  const [busy, setBusy] = useState(false);

  const mission = missionId || (app.missions[0] ? app.missions[0].id : "");
  const canStartImmediately =
    assignee.startsWith("pilot:") ||
    (assignee.startsWith("crew:") && !!app.crews.find((c) => `crew:${c.id}` === assignee)?.members?.some((m) => m.member_type === "pilot"));
  const startNow = canStartImmediately && startImmediately;
  const setAssigneeAndStart = (value: string) => {
    setAssignee(value);
    setStartImmediately(
      value.startsWith("pilot:") ||
      (value.startsWith("crew:") && !!app.crews.find((c) => `crew:${c.id}` === value)?.members?.some((m) => m.member_type === "pilot")),
    );
  };

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!title.trim() || !mission) return;
    setBusy(true);
    const a =
      assignee === "me"
        ? { assignee_type: "user", assignee_id: app.user.id }
        : (() => { const [k, id] = assignee.split(":"); return { assignee_type: k, assignee_id: id }; })();
    const op = await app.createOperation({ title, body, mission_id: mission, start_immediately: startNow, ...a });
    setBusy(false);
    if (op) { setTitle(""); setBody(""); setOpen(false); app.openOperation(op.id); }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm"><Plus /> New operation</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New operation</DialogTitle>
        </DialogHeader>
        {app.missions.length === 0 ? (
          <p className="py-4 text-sm text-muted-foreground">
            Create a mission first — every operation belongs to one. Head to the Missions section to add one.
          </p>
        ) : (
        <form onSubmit={submit} className="space-y-3">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Title" autoFocus />
          <Textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="What should happen? (the prompt for the assigned pilot)"
            rows={4}
          />
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">Assignee</Label>
              <Select value={assignee} onValueChange={setAssigneeAndStart}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="me">Me</SelectItem>
                  {app.members.filter((m) => m.id !== app.user.id).map((m) => <SelectItem key={`u${m.id}`} value={`user:${m.id}`}>🧑 {m.name || m.email}</SelectItem>)}
                  {app.pilots.map((p) => <SelectItem key={`p${p.kind}`} value={`pilot:${p.kind}`} disabled={p.rovers === 0}><span className="flex items-center gap-2"><PilotIcon kind={p.kind} /> {pilotLabel(p.kind)}{p.rovers === 0 && " — no rover"}</span></SelectItem>)}
                  {app.crews.map((c) => <SelectItem key={`c${c.id}`} value={`crew:${c.id}`}>👥 {c.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">Mission</Label>
              <Select value={mission} onValueChange={setMissionId}>
                <SelectTrigger><SelectValue placeholder="Select a mission" /></SelectTrigger>
                <SelectContent>
                  {app.missions.map((m) => <SelectItem key={m.id} value={String(m.id)}><span className="font-mono text-xs">{m.key}</span> · {m.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <label className="flex items-center justify-between gap-3 text-xs">
            <span>
              <span className="font-medium text-foreground">Run automatically</span>
              <span className="block text-muted-foreground">
                {canStartImmediately ? "Dispatch when created" : "Human-only assignments stay in Backlog"}
              </span>
            </span>
            <input
              type="checkbox"
              className="peer sr-only"
              checked={startNow}
              disabled={!canStartImmediately}
              onChange={(e) => setStartImmediately(e.target.checked)}
            />
            <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-disabled:opacity-50" />
          </label>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" disabled={busy}>{busy ? "Creating…" : "Create"}</Button>
          </div>
        </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
