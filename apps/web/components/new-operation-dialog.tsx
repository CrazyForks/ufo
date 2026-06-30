"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2, Paperclip, Plus } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { AssetDeleteDialog } from "@/components/asset-delete-dialog";
import { AssetChipStrip } from "@/components/asset-display";
import { appendAssetLink, removeAssetLink } from "@/lib/assets";
import { del } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { CrewOption, PilotOption } from "@/components/assignee-select";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { pilotLabel, userLabel } from "@/lib/labels";
import { DRAFT_SAVE_DELAY_SECONDS } from "@/lib/view";
import type { Asset } from "@/lib/types";

const OPERATION_CREATE_DRAFT_KEY = "ufo.operationCreateDraft";

type OperationCreateDraft = {
  title: string;
  body: string;
  assignee: string;
  startImmediately: boolean;
  subOperationsEnabled: boolean;
  missionId: string;
};

function defaultOperationCreateDraft(): OperationCreateDraft {
  return { title: "", body: "", assignee: "me", startImmediately: true, subOperationsEnabled: true, missionId: "" };
}

function readOperationCreateDraft(): OperationCreateDraft {
  if (typeof window === "undefined") return defaultOperationCreateDraft();
  try {
    return { ...defaultOperationCreateDraft(), ...JSON.parse(sessionStorage.getItem(OPERATION_CREATE_DRAFT_KEY) || "{}") };
  } catch {
    return defaultOperationCreateDraft();
  }
}

function writeOperationCreateDraft(draft: OperationCreateDraft) {
  const hasDraft = draft.title.trim() || draft.body.trim() || draft.assignee !== "me" || !draft.startImmediately || !draft.subOperationsEnabled || draft.missionId;
  const saved = sessionStorage.getItem(OPERATION_CREATE_DRAFT_KEY);
  if (!hasDraft) {
    if (saved != null) sessionStorage.removeItem(OPERATION_CREATE_DRAFT_KEY);
    return;
  }
  const next = JSON.stringify(draft);
  if (saved !== next) sessionStorage.setItem(OPERATION_CREATE_DRAFT_KEY, next);
}

export function NewOperationDialog() {
  const app = useApp();
  const savedDraft = readOperationCreateDraft();
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState(savedDraft.title);
  const [body, setBody] = useState(savedDraft.body);
  const [assignee, setAssignee] = useState(savedDraft.assignee);
  const [startImmediately, setStartImmediately] = useState(savedDraft.startImmediately);
  const [subOperationsEnabled, setSubOperationsEnabled] = useState(savedDraft.subOperationsEnabled);
  const [missionId, setMissionId] = useState<string>(savedDraft.missionId);
  const [creating, setCreating] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [assets, setAssets] = useState<Asset[]>([]);
  const [assetDeleteTarget, setAssetDeleteTarget] = useState<Asset | null>(null);
  const [assetDeletingId, setAssetDeletingId] = useState<string | null>(null);
  const [assetDeleteError, setAssetDeleteError] = useState<string | null>(null);
  const uploadRef = useRef<HTMLInputElement>(null);
  const draftRef = useRef(savedDraft);

  const mission = missionId || (app.missions[0] ? app.missions[0].id : "");
  const sortedCrews = [...app.crews].sort((a, b) => a.name.localeCompare(b.name));
  const sortedPilots = [...app.pilots].sort((a, b) => pilotLabel(a.kind).localeCompare(pilotLabel(b.kind)));
  const sortedMembers = app.members.filter((m) => m.id !== app.user.id).sort((a, b) => (a.name || a.email).localeCompare(b.name || b.email));
  const canStartImmediately =
    assignee.startsWith("pilot:") ||
    (assignee.startsWith("crew:") && !!app.crews.find((c) => `crew:${c.id}` === assignee)?.members?.some((m) => m.member_type === "pilot"));
  const startNow = canStartImmediately && startImmediately;
  const setAssigneeAndStart = (value: string) => {
    const nextStartImmediately =
      value.startsWith("pilot:") ||
      (value.startsWith("crew:") && !!app.crews.find((c) => `crew:${c.id}` === value)?.members?.some((m) => m.member_type === "pilot"));
    draftRef.current = { ...draftRef.current, assignee: value, startImmediately: nextStartImmediately };
    setAssignee(value);
    setStartImmediately(nextStartImmediately);
  };
  draftRef.current = { title, body, assignee, startImmediately, subOperationsEnabled, missionId };
  function saveCurrentDraft() {
    writeOperationCreateDraft(draftRef.current);
  }
  useEffect(() => {
    const id = window.setTimeout(saveCurrentDraft, DRAFT_SAVE_DELAY_SECONDS * 1000);
    return () => window.clearTimeout(id);
  }, [title, body, assignee, startImmediately, subOperationsEnabled, missionId]);
  useEffect(() => {
    window.addEventListener("pagehide", saveCurrentDraft);
    window.addEventListener("beforeunload", saveCurrentDraft);
    return () => {
      window.removeEventListener("pagehide", saveCurrentDraft);
      window.removeEventListener("beforeunload", saveCurrentDraft);
      saveCurrentDraft();
    };
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (creating || !title.trim() || !mission) return;
    setCreating(true);
    const a =
      assignee === "me"
        ? { assignee_type: "user", assignee_id: app.user.id }
        : (() => { const [k, id] = assignee.split(":"); return { assignee_type: k, assignee_id: id }; })();
    const op = await app.createOperation({ title, body, mission_id: mission, start_immediately: startNow, sub_operations_enabled: subOperationsEnabled, asset_ids: assets.map((asset) => asset.id), ...a });
    setCreating(false);
    if (op) { sessionStorage.removeItem(OPERATION_CREATE_DRAFT_KEY); draftRef.current = defaultOperationCreateDraft(); setTitle(""); setBody(""); setAssignee("me"); setStartImmediately(true); setSubOperationsEnabled(true); setMissionId(""); setAssets([]); setOpen(false); app.openOperation(op.id); }
  }

  async function onFiles(files: FileList | null) {
    const selected = Array.from(files ?? []);
    if (selected.length === 0 || uploading) return;
    setUploading(true);
    try {
      for (const file of selected) {
        const asset = await app.uploadAsset(file);
        if (asset) {
          setAssets((prev) => [...prev, asset]);
          setBody((prev) => {
            const next = appendAssetLink(prev, asset);
            draftRef.current = { ...draftRef.current, body: next };
            return next;
          });
        }
      }
    } finally {
      setUploading(false);
      if (uploadRef.current) uploadRef.current.value = "";
    }
  }

  async function deleteUploadedAsset(asset: Asset) {
    if (assetDeletingId) return;
    setAssetDeletingId(asset.id);
    setAssetDeleteError(null);
    try {
      const res = await del(`/api/v1/assets/${asset.id}`);
      if (!res.ok) {
        setAssetDeleteError("Could not delete this file.");
        return;
      }
      setAssets((prev) => prev.filter((item) => item.id !== asset.id));
      setBody((prev) => {
        const next = removeAssetLink(prev, asset);
        draftRef.current = { ...draftRef.current, body: next };
        return next;
      });
      setAssetDeleteTarget(null);
    } finally {
      setAssetDeletingId(null);
    }
  }

  return (
    <>
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
        <form
          onSubmit={submit}
          onKeyDown={(e) => {
            if (e.nativeEvent.isComposing) return;
            if (e.key === "Enter" && !e.shiftKey && e.target instanceof HTMLTextAreaElement) {
              e.preventDefault();
              e.currentTarget.requestSubmit();
            }
          }}
          className="space-y-3"
        >
          <Input value={title} onChange={(e) => { draftRef.current = { ...draftRef.current, title: e.target.value }; setTitle(e.target.value); }} placeholder="Title" autoFocus />
          <div className="space-y-1.5">
            <Textarea
              value={body}
              onChange={(e) => { draftRef.current = { ...draftRef.current, body: e.target.value }; setBody(e.target.value); }}
              placeholder="What should happen? (the prompt for the assigned pilot)"
              rows={4}
            />
            <input ref={uploadRef} type="file" multiple className="sr-only" onChange={(e) => onFiles(e.target.files)} />
            <div className="flex items-center gap-1">
              <Button type="button" variant="ghost" size="icon-sm" className="text-muted-foreground" title="Upload files" aria-label="Upload files" disabled={uploading} onClick={() => uploadRef.current?.click()}>
                {uploading ? <Loader2 className="size-3 animate-spin" /> : <Paperclip className="size-3" />}
              </Button>
            </div>
            <AssetChipStrip assets={assets} deletingAssetId={assetDeletingId} onDelete={(asset) => { setAssetDeleteError(null); setAssetDeleteTarget(asset); }} onInsert={(asset) => setBody((prev) => {
              const next = appendAssetLink(prev, asset);
              draftRef.current = { ...draftRef.current, body: next };
              return next;
            })} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">Assignee</Label>
              <Select value={assignee} onValueChange={setAssigneeAndStart}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="me">{userLabel(app.user)}</SelectItem>
                  {sortedCrews.map((c) => <SelectItem key={`c${c.id}`} value={`crew:${c.id}`}><CrewOption crew={c} crewIcon="emoji" /></SelectItem>)}
                  {sortedPilots.map((p) => <SelectItem key={`p${p.kind}`} value={`pilot:${p.kind}`} disabled={p.rovers === 0}><PilotOption kind={p.kind} unavailable={p.rovers === 0} /></SelectItem>)}
                  {sortedMembers.map((m) => <SelectItem key={`u${m.id}`} value={`user:${m.id}`}>🧑 {m.name || m.email}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">Mission</Label>
              <Select value={mission} onValueChange={(value) => { draftRef.current = { ...draftRef.current, missionId: value }; setMissionId(value); }}>
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
              onChange={(e) => { draftRef.current = { ...draftRef.current, startImmediately: e.target.checked }; setStartImmediately(e.target.checked); }}
            />
            <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-disabled:opacity-50" />
          </label>
          <label className="flex items-center justify-between gap-3 text-xs">
            <span>
              <span className="font-medium text-foreground">Allow sub-operations</span>
              <span className="block text-muted-foreground">Crew captains may split this operation</span>
            </span>
            <input
              type="checkbox"
              className="peer sr-only"
              checked={subOperationsEnabled}
              onChange={(e) => { draftRef.current = { ...draftRef.current, subOperationsEnabled: e.target.checked }; setSubOperationsEnabled(e.target.checked); }}
            />
            <span className="relative h-5 w-9 shrink-0 rounded-full bg-muted transition after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-background after:shadow after:transition after:content-[''] peer-checked:bg-brand peer-checked:after:translate-x-4 peer-focus-visible:ring-2 peer-focus-visible:ring-ring" />
          </label>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" onClick={() => { sessionStorage.removeItem(OPERATION_CREATE_DRAFT_KEY); draftRef.current = defaultOperationCreateDraft(); setTitle(""); setBody(""); setAssignee("me"); setStartImmediately(true); setSubOperationsEnabled(true); setMissionId(""); setAssets([]); setOpen(false); }}>Cancel</Button>
            <Button type="submit" disabled={creating}>{creating ? "Creating…" : "Create"}</Button>
          </div>
        </form>
        )}
      </DialogContent>
    </Dialog>
    <AssetDeleteDialog
      asset={assetDeleteTarget}
      open={assetDeleteTarget != null}
      deleting={assetDeletingId != null}
      error={assetDeleteError}
      onOpenChange={(next) => { if (!next) { setAssetDeleteTarget(null); setAssetDeleteError(null); } }}
      onConfirm={deleteUploadedAsset}
    />
    </>
  );
}
