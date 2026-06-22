"use client";

import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { del, getJSON, patchJSON, postJSON, putJSON } from "@/lib/api";
import { WSClient } from "@/lib/ws";
import { parseAppPath } from "@/lib/routes";
import type {
  EnrollmentCode, Pilot, Comment, Crew, Fleet, Invitation, Label, Member, MyInvite, OperationReference, Signal, Mission, Operation,
  OperationDetail, Rover, Run, RunDetail, User,
} from "@/lib/types";

type Ctx = {
  user: User;
  fleets: Fleet[];
  fleet: string;
  switchFleet: (id: string) => void;
  createFleet: (name: string) => Promise<void>;
  updateFleet: (id: string, name: string) => Promise<boolean>;
  signOut: () => void;
  // data (board fetches its own pages; provider holds the small lists)
  missions: Mission[];
  missionCounts: Record<string, number>;
  pilots: Pilot[];
  crews: Crew[];
  labels: Label[];
  rovers: Rover[];
  enrollmentCodes: EnrollmentCode[];
  signals: Signal[];
  newEnrollmentCode: string | null;
  members: Member[];
  myRole: string;
  fleetInvites: Invitation[];
  myInvites: MyInvite[];
  // boardTick increments whenever operations change → the board refetches.
  boardTick: number;
  // selection
  selectedOperation: string | null;
  openOperation: (id: string | null) => void;
  operationDetail: OperationDetail | null;
  selectedRun: string | null;
  setSelectedRun: (id: string | null) => void;
  runDetail: RunDetail | null;
  // actions
  createOperation: (i: { title: string; body: string; mission_id: string | null; assignee_type: string | null; assignee_id: string | null; start_immediately?: boolean; required_tags?: string[]; excluded_tags?: string[]; priority?: number; main_operation_id?: string | null; start_date?: string | null; due_date?: string | null }) => Promise<Operation | null>;
  setOperationTags: (operationId: string, required_tags: string[], excluded_tags: string[]) => Promise<void>;
  setPriority: (operationId: string, priority: number) => Promise<void>;
  setDates: (operationId: string, start_date: string | null, due_date: string | null) => Promise<void>;
  setMainOperation: (operationId: string, main_operation_id: string | null) => Promise<void>;
  setArchived: (operationId: string, archived: boolean) => Promise<void>;
  createLabel: (name: string, color: string) => Promise<Label | null>;
  deleteLabel: (id: string) => Promise<void>;
  attachLabel: (operationId: string, labelId: string) => Promise<void>;
  detachLabel: (operationId: string, labelId: string) => Promise<void>;
  addPullRequest: (operationId: string, url: string, title: string) => Promise<void>;
  deletePullRequest: (pullRequestId: string, operationId: string) => Promise<void>;
  addRelation: (operationId: string, kind: string, target: string) => Promise<void>;
  removeRelation: (relationId: string, operationId: string) => Promise<void>;
  searchOperations: (q: string) => Promise<OperationReference[]>;
  react: (kind: "comments" | "operations", id: string, emoji: string, operationId: string, on?: boolean) => Promise<void>;
  reassign: (operationId: string, assignee_type: string | null, assignee_id: string | null) => Promise<void>;
  runOperation: (operationId: string, message?: string) => Promise<void>;
  cancelRun: (runId: string, operationId: string) => Promise<void>;
  moveOperation: (operationId: string, status: string) => Promise<void>;
  addComment: (operationId: string, body: string) => Promise<void>;
  addCrew: (name: string) => Promise<void>;
  renameCrew: (id: string, name: string) => Promise<void>;
  delCrew: (id: string) => Promise<void>;
  addMission: (name: string, key: string) => Promise<boolean>;
  updateMission: (id: string, name: string, key: string) => Promise<boolean>;
  addMember: (crewId: string, value: string, role: string, userId: string) => Promise<void>;
  removeMember: (crewId: string, member_type: string, member_id: string) => Promise<void>;
  createEnrollmentCode: (i: { name?: string; expiresAt?: string; uses?: number }) => Promise<void>;
  revokeRover: (id: string) => Promise<void>;
  renameRover: (id: string, name: string) => Promise<void>;
  setRoverTags: (id: string, tags: string[]) => Promise<void>;
  revokeEnrollmentCode: (id: string) => Promise<void>;
  openSignal: (it: Signal) => Promise<void>;
  archiveSignal: (id: string) => Promise<void>;
  invite: (email: string, role: string) => Promise<boolean>;
  revokeInvite: (id: string) => Promise<void>;
  acceptInvite: (id: string, fleetId: string) => Promise<void>;
  declineInvite: (id: string) => Promise<void>;
  setMemberRole: (userId: string, role: string) => Promise<void>;
  removeFleetMember: (userId: string) => Promise<void>;
};

const AppCtx = createContext<Ctx | null>(null);
export const useApp = () => {
  const c = useContext(AppCtx);
  if (!c) throw new Error("useApp outside provider");
  return c;
};

export function AppProvider({ user, fleets: initialFleets, initialFleet, children }: { user: User; fleets: Fleet[]; initialFleet: string; children: React.ReactNode }) {
  const [fleets, setFleets] = useState<Fleet[]>(initialFleets);
  const [fleet, setFleet] = useState(initialFleet);
  const [missions, setMissions] = useState<Mission[]>([]);
  const [missionCounts, setMissionCounts] = useState<Record<string, number>>({});
  const [pilots, setPilots] = useState<Pilot[]>([]);
  const [crews, setCrews] = useState<Crew[]>([]);
  const [labels, setLabels] = useState<Label[]>([]);
  const [rovers, setRovers] = useState<Rover[]>([]);
  const [enrollmentCodes, setEnrollmentCodes] = useState<EnrollmentCode[]>([]);
  const [signals, setSignals] = useState<Signal[]>([]);
  const [newEnrollmentCode, setNewEnrollmentCode] = useState<string | null>(null);
  const [newEnrollmentCodeId, setNewEnrollmentCodeId] = useState<string | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [fleetInvites, setFleetInvites] = useState<Invitation[]>([]);
  const [myInvites, setMyInvites] = useState<MyInvite[]>([]);
  const [boardTick, setBoardTick] = useState(0);
  const myRole = members.find((m) => m.id === user.id)?.role ?? "member";

  const [selectedOperation, setSelectedOperation] = useState<string | null>(() =>
    typeof window === "undefined" ? null : parseAppPath(window.location.pathname).operationId,
  );
  const [operationDetail, setOperationDetail] = useState<OperationDetail | null>(null);
  const [selectedRun, setSelectedRun] = useState<string | null>(null);
  const [runDetail, setRunDetail] = useState<RunDetail | null>(null);

  const operationRef = useRef<string | null>(null); operationRef.current = selectedOperation;
  const runRef = useRef<string | null>(null); runRef.current = selectedRun;
  const bumpBoard = useCallback(() => setBoardTick((t) => t + 1), []);

  const loadSignals = useCallback(async (f: string) => { const d = await getJSON<Signal[]>(`/api/v1/signals?fleet=${f}`); if (d) setSignals(d); }, []);
  const loadRovers = useCallback(async (f: string) => { const d = await getJSON<Rover[]>(`/api/v1/rovers?fleet=${f}`); if (d) setRovers(d); }, []);
  const loadMissionCounts = useCallback(async (f: string) => { const d = await getJSON<Record<string, number>>(`/api/v1/missions/counts?fleet=${f}`); if (d) setMissionCounts(d); }, []);
  const loadMembers = useCallback(async (f: string) => {
    const [m, inv] = await Promise.all([getJSON<Member[]>(`/api/v1/members?fleet=${f}`), getJSON<Invitation[]>(`/api/v1/invitations?fleet=${f}`)]);
    if (m) setMembers(m);
    setFleetInvites(inv ?? []); // null unless the caller is an owner/admin
  }, []);
  const loadMyInvites = useCallback(async () => { const d = await getJSON<MyInvite[]>(`/api/v1/invitations/mine`); setMyInvites(d ?? []); }, []);
  const loadMeta = useCallback(async (f: string) => {
    const [m, a, c, rv, t, lb] = await Promise.all([
      getJSON<Mission[]>(`/api/v1/missions?fleet=${f}`),
      getJSON<Pilot[]>(`/api/v1/pilots?fleet=${f}`),
      getJSON<Crew[]>(`/api/v1/crews?fleet=${f}`),
      getJSON<Rover[]>(`/api/v1/rovers?fleet=${f}`),
      getJSON<EnrollmentCode[]>(`/api/v1/enrollment-codes?fleet=${f}`),
      getJSON<Label[]>(`/api/v1/labels?fleet=${f}`),
    ]);
    if (m) setMissions(m);
    if (a) setPilots(a);
    if (c) setCrews(c);
    if (lb) setLabels(lb);
    if (rv) setRovers(rv);
    if (t) {
      setEnrollmentCodes(t);
      setNewEnrollmentCodeId((id) => {
        if (id != null && !t.some((it) => it.id === id)) {
          setNewEnrollmentCode(null);
          return null;
        }
        return id;
      });
    }
  }, []);
  const loadOperationDetail = useCallback(async (f: string, id: string) => { const d = await getJSON<OperationDetail>(`/api/v1/operations/${id}?fleet=${f}`); if (d) setOperationDetail(d); }, []);
  const loadRunDetail = useCallback(async (f: string, id: string) => { const d = await getJSON<RunDetail>(`/api/v1/runs/${id}?fleet=${f}`); if (d) setRunDetail(d); }, []);

  useEffect(() => {
    const resync = () => {
      loadMeta(fleet); loadSignals(fleet); loadMissionCounts(fleet); loadMembers(fleet); loadMyInvites(); bumpBoard();
      if (operationRef.current != null) loadOperationDetail(fleet, operationRef.current);
      if (runRef.current != null) loadRunDetail(fleet, runRef.current);
    };
    resync();
    const ws = new WSClient(fleet);
    ws.onReconnect(resync);
    ws.onEvent((type) => {
      switch (type) {
        case "operation":
          bumpBoard(); loadMissionCounts(fleet);
          if (operationRef.current != null) loadOperationDetail(fleet, operationRef.current);
          break;
        case "comment":
          if (operationRef.current != null) loadOperationDetail(fleet, operationRef.current);
          break;
        case "run":
          bumpBoard(); loadRovers(fleet);
          if (operationRef.current != null) loadOperationDetail(fleet, operationRef.current);
          if (runRef.current != null) loadRunDetail(fleet, runRef.current);
          break;
        case "run_message":
          if (runRef.current != null) loadRunDetail(fleet, runRef.current);
          break;
        case "signal":
          loadSignals(fleet);
          break;
        case "rover":
          loadMeta(fleet);
          break;
      }
    });
    ws.connect();
    return () => ws.close();
  }, [fleet, loadMeta, loadSignals, loadMissionCounts, loadRovers, loadMembers, loadMyInvites, loadOperationDetail, loadRunDetail, bumpBoard]);

  useEffect(() => { if (selectedOperation != null) loadOperationDetail(fleet, selectedOperation); else setOperationDetail(null); }, [fleet, selectedOperation, loadOperationDetail]);
  useEffect(() => { if (selectedRun != null) loadRunDetail(fleet, selectedRun); else setRunDetail(null); }, [fleet, selectedRun, loadRunDetail]);

  const switchFleet = useCallback((id: string) => {
    localStorage.setItem("ufo.fleet", id);
    setSelectedOperation(null); setOperationDetail(null); setSelectedRun(null); setRunDetail(null); setNewEnrollmentCode(null); setNewEnrollmentCodeId(null);
    setFleet(id);
  }, []);
  const signOut = useCallback(async () => { await postJSON(`/api/v1/auth/logout`); window.location.href = "/login"; }, []);
  const openOperation = useCallback((id: string | null) => { setSelectedOperation(id); setSelectedRun(null); setRunDetail(null); }, []);

  const fail = (res: Response, fallback: string) => res.json().then((d) => toast.error(d.error || fallback)).catch(() => toast.error(fallback));

  const createFleet: Ctx["createFleet"] = useCallback(async (name) => {
    const res = await postJSON(`/api/v1/fleets`, { name });
    if (!res.ok) { await fail(res, "Create fleet failed"); return; }
    const f = (await res.json()) as Fleet;
    setFleets((prev) => [...prev, f]);
    switchFleet(f.id);
    toast.success("Fleet created");
  }, [switchFleet]);

  const updateFleet: Ctx["updateFleet"] = useCallback(async (id, name) => {
    const res = await patchJSON(`/api/v1/fleets/${id}`, { name });
    if (!res.ok) { await fail(res, "Rename fleet failed"); return false; }
    const f = (await res.json()) as Fleet;
    setFleets((prev) => prev.map((it) => it.id === f.id ? f : it));
    toast.success("Fleet renamed");
    return true;
  }, []);

  const createOperation: Ctx["createOperation"] = useCallback(async (input) => {
    const res = await postJSON(`/api/v1/operations?fleet=${fleet}`, input);
    if (!res.ok) { await fail(res, "Create failed"); return null; }
    const op = (await res.json()) as Operation;
    bumpBoard(); loadMissionCounts(fleet);
    toast.success("Operation created");
    return op;
  }, [fleet, bumpBoard, loadMissionCounts]);

  const reassign: Ctx["reassign"] = useCallback(async (operationId, assignee_type, assignee_id) => {
    await patchJSON(`/api/v1/operations/${operationId}?fleet=${fleet}`, { assignee_type, assignee_id });
    bumpBoard(); loadOperationDetail(fleet, operationId);
  }, [fleet, bumpBoard, loadOperationDetail]);

  const setOperationTags: Ctx["setOperationTags"] = useCallback(async (operationId, required_tags, excluded_tags) => {
    await patchJSON(`/api/v1/operations/${operationId}?fleet=${fleet}`, { required_tags, excluded_tags });
    loadOperationDetail(fleet, operationId);
  }, [fleet, loadOperationDetail]);

  const setPriority: Ctx["setPriority"] = useCallback(async (operationId, priority) => {
    await patchJSON(`/api/v1/operations/${operationId}?fleet=${fleet}`, { priority });
    bumpBoard(); loadOperationDetail(fleet, operationId);
  }, [fleet, bumpBoard, loadOperationDetail]);
  const setDates: Ctx["setDates"] = useCallback(async (operationId, start_date, due_date) => {
    await patchJSON(`/api/v1/operations/${operationId}?fleet=${fleet}`, { start_date, due_date });
    loadOperationDetail(fleet, operationId);
  }, [fleet, loadOperationDetail]);
  const setMainOperation: Ctx["setMainOperation"] = useCallback(async (operationId, main_operation_id) => {
    await patchJSON(`/api/v1/operations/${operationId}?fleet=${fleet}`, { main_operation_id });
    bumpBoard(); loadOperationDetail(fleet, operationId);
  }, [fleet, bumpBoard, loadOperationDetail]);
  const setArchived: Ctx["setArchived"] = useCallback(async (operationId, archived) => {
    await patchJSON(`/api/v1/operations/${operationId}?fleet=${fleet}`, { archived });
    bumpBoard(); loadOperationDetail(fleet, operationId);
  }, [fleet, bumpBoard, loadOperationDetail]);
  const createLabel: Ctx["createLabel"] = useCallback(async (name, color) => {
    const res = await postJSON(`/api/v1/labels?fleet=${fleet}`, { name, color });
    if (!res.ok) { await fail(res, "Create label failed"); return null; }
    const l = (await res.json()) as Label;
    setLabels((prev) => [...prev, l].sort((a, b) => a.name.localeCompare(b.name)));
    return l;
  }, [fleet]);
  const deleteLabel: Ctx["deleteLabel"] = useCallback(async (id) => {
    await del(`/api/v1/labels/${id}?fleet=${fleet}`); loadMeta(fleet); bumpBoard();
  }, [fleet, loadMeta, bumpBoard]);
  const attachLabel: Ctx["attachLabel"] = useCallback(async (operationId, labelId) => {
    await putJSON(`/api/v1/operations/${operationId}/labels/${labelId}?fleet=${fleet}`);
    bumpBoard(); loadOperationDetail(fleet, operationId);
  }, [fleet, bumpBoard, loadOperationDetail]);
  const detachLabel: Ctx["detachLabel"] = useCallback(async (operationId, labelId) => {
    await del(`/api/v1/operations/${operationId}/labels/${labelId}?fleet=${fleet}`);
    bumpBoard(); loadOperationDetail(fleet, operationId);
  }, [fleet, bumpBoard, loadOperationDetail]);
  const addPullRequest: Ctx["addPullRequest"] = useCallback(async (operationId, url, title) => {
    await postJSON(`/api/v1/operations/${operationId}/pull-requests?fleet=${fleet}`, { url, title });
    loadOperationDetail(fleet, operationId);
  }, [fleet, loadOperationDetail]);
  const deletePullRequest: Ctx["deletePullRequest"] = useCallback(async (pullRequestId, operationId) => {
    await del(`/api/v1/pull-requests/${pullRequestId}?fleet=${fleet}`); loadOperationDetail(fleet, operationId);
  }, [fleet, loadOperationDetail]);
  const addRelation: Ctx["addRelation"] = useCallback(async (operationId, kind, target) => {
    await postJSON(`/api/v1/operations/${operationId}/relations?fleet=${fleet}`, { kind, target }); loadOperationDetail(fleet, operationId);
  }, [fleet, loadOperationDetail]);
  const removeRelation: Ctx["removeRelation"] = useCallback(async (relationId, operationId) => {
    await del(`/api/v1/relations/${relationId}?fleet=${fleet}`); loadOperationDetail(fleet, operationId);
  }, [fleet, loadOperationDetail]);
  const searchOperations: Ctx["searchOperations"] = useCallback(async (q) => {
    return (await getJSON<OperationReference[]>(`/api/v1/operations/search?fleet=${fleet}&q=${encodeURIComponent(q)}`)) ?? [];
  }, [fleet]);
  const react: Ctx["react"] = useCallback(async (kind, id, emoji, operationId, on = true) => {
    const path = `/api/v1/${kind}/${id}/reactions/${encodeURIComponent(emoji)}?fleet=${fleet}`;
    await (on ? putJSON(path) : del(path));
    loadOperationDetail(fleet, operationId);
  }, [fleet, loadOperationDetail]);

  const runOperation: Ctx["runOperation"] = useCallback(async (operationId, message) => {
    const res = await postJSON(`/api/v1/operations/${operationId}/run?fleet=${fleet}`, message ? { message } : undefined);
    if (!res.ok) { await fail(res, "Run failed"); return; }
    bumpBoard(); loadOperationDetail(fleet, operationId); toast.success("Run dispatched");
  }, [fleet, bumpBoard, loadOperationDetail]);
  const cancelRun: Ctx["cancelRun"] = useCallback(async (runId, operationId) => {
    const res = await postJSON(`/api/v1/runs/${runId}/cancel?fleet=${fleet}`);
    if (!res.ok) { await fail(res, "Stop failed"); return; }
    bumpBoard(); loadOperationDetail(fleet, operationId); toast.success("Run stopped");
  }, [fleet, bumpBoard, loadOperationDetail]);

  // The board applies the move optimistically; here we persist then bump to reconcile.
  const moveOperation: Ctx["moveOperation"] = useCallback(async (operationId, status) => {
    const res = await patchJSON(`/api/v1/operations/${operationId}?fleet=${fleet}`, { status });
    if (!res.ok) await fail(res, "Move failed");
    bumpBoard(); loadSignals(fleet);
  }, [fleet, bumpBoard, loadSignals]);

  const addComment: Ctx["addComment"] = useCallback(async (operationId, body) => {
    await postJSON(`/api/v1/operations/${operationId}/comments?fleet=${fleet}`, { body });
    loadOperationDetail(fleet, operationId);
  }, [fleet, loadOperationDetail]);

  const addCrew: Ctx["addCrew"] = useCallback(async (name) => { await postJSON(`/api/v1/crews?fleet=${fleet}`, { name }); loadMeta(fleet); }, [fleet, loadMeta]);
  const renameCrew: Ctx["renameCrew"] = useCallback(async (id, name) => {
    const res = await patchJSON(`/api/v1/crews/${id}?fleet=${fleet}`, { name });
    if (!res.ok) { await fail(res, "Rename crew failed"); return; }
    loadMeta(fleet);
  }, [fleet, loadMeta]);
  const delCrew: Ctx["delCrew"] = useCallback(async (id) => { await del(`/api/v1/crews/${id}?fleet=${fleet}`); loadMeta(fleet); }, [fleet, loadMeta]);
  const addMission: Ctx["addMission"] = useCallback(async (name, key) => {
    const res = await postJSON(`/api/v1/missions?fleet=${fleet}`, { name, key });
    if (!res.ok) { await fail(res, "Add mission failed"); return false; }
    loadMeta(fleet);
    return true;
  }, [fleet, loadMeta]);
  const updateMission: Ctx["updateMission"] = useCallback(async (id, name, key) => {
    const res = await patchJSON(`/api/v1/missions/${id}?fleet=${fleet}`, { name, key });
    if (!res.ok) { await fail(res, "Update mission failed"); return false; }
    loadMeta(fleet); bumpBoard();
    return true;
  }, [fleet, loadMeta, bumpBoard]);
  const addMember: Ctx["addMember"] = useCallback(async (crewId, value, role, userId) => {
    const [member_type, id] = value === "me" ? ["user", userId] : value.split(":");
    await putJSON(`/api/v1/crews/${crewId}/members/${member_type}/${id}?fleet=${fleet}`, { role });
    loadMeta(fleet);
  }, [fleet, loadMeta]);
  const removeMember: Ctx["removeMember"] = useCallback(async (crewId, member_type, member_id) => {
    await del(`/api/v1/crews/${crewId}/members/${member_type}/${member_id}?fleet=${fleet}`);
    loadMeta(fleet);
  }, [fleet, loadMeta]);

  const createEnrollmentCode: Ctx["createEnrollmentCode"] = useCallback(async ({ name = "", expiresAt = "", uses }) => {
    const body: Record<string, unknown> = {};
    if (uses && uses > 1) {
      body.uses = uses;
      body.name = name;
      if (expiresAt) body.expires_at = expiresAt;
    }
    const res = await postJSON(`/api/v1/enrollment-codes?fleet=${fleet}`, body);
    if (res.ok) {
      const code = (await res.json()) as EnrollmentCode;
      setNewEnrollmentCode(code.code);
      setNewEnrollmentCodeId(code.id);
      loadMeta(fleet);
    }
    else await fail(res, "Enrollment code failed");
  }, [fleet, loadMeta]);
  const revokeRover: Ctx["revokeRover"] = useCallback(async (id) => { await del(`/api/v1/rovers/${id}?fleet=${fleet}`); loadMeta(fleet); }, [fleet, loadMeta]);
  const renameRover: Ctx["renameRover"] = useCallback(async (id, name) => {
    const res = await patchJSON(`/api/v1/rovers/${id}?fleet=${fleet}`, { name });
    if (!res.ok) { await fail(res, "Rename rover failed"); return; }
    loadMeta(fleet);
  }, [fleet, loadMeta]);
  const setRoverTags: Ctx["setRoverTags"] = useCallback(async (id, tags) => {
    const res = await patchJSON(`/api/v1/rovers/${id}?fleet=${fleet}`, { tags });
    if (!res.ok) { await fail(res, "Tag update failed"); return; }
    loadMeta(fleet);
  }, [fleet, loadMeta]);
  const revokeEnrollmentCode: Ctx["revokeEnrollmentCode"] = useCallback(async (id) => { await del(`/api/v1/enrollment-codes/${id}?fleet=${fleet}`); loadMeta(fleet); }, [fleet, loadMeta]);

  const openSignal: Ctx["openSignal"] = useCallback(async (it) => {
    if (!it.read) { await patchJSON(`/api/v1/signals/${it.id}?fleet=${fleet}`, { read: true }); loadSignals(fleet); }
    if (it.operation_id != null) openOperation(it.operation_id);
  }, [fleet, loadSignals, openOperation]);
  const archiveSignal: Ctx["archiveSignal"] = useCallback(async (id) => { await patchJSON(`/api/v1/signals/${id}?fleet=${fleet}`, { archived: true }); loadSignals(fleet); }, [fleet, loadSignals]);

  const invite: Ctx["invite"] = useCallback(async (email, role) => {
    const res = await postJSON(`/api/v1/invitations?fleet=${fleet}`, { email, role });
    if (!res.ok) { await fail(res, "Invite failed"); return false; }
    loadMembers(fleet); toast.success("Invitation sent");
    return true;
  }, [fleet, loadMembers]);
  const revokeInvite: Ctx["revokeInvite"] = useCallback(async (id) => { await del(`/api/v1/invitations/${id}?fleet=${fleet}`); loadMembers(fleet); }, [fleet, loadMembers]);
  const acceptInvite: Ctx["acceptInvite"] = useCallback(async (id, fleetId) => {
    const res = await postJSON(`/api/v1/invitations/${id}/accept`);
    if (!res.ok) { await fail(res, "Accept failed"); return; }
    localStorage.setItem("ufo.fleet", fleetId);
    window.location.reload();
  }, []);
  const declineInvite: Ctx["declineInvite"] = useCallback(async (id) => { await postJSON(`/api/v1/invitations/${id}/decline`); loadMyInvites(); }, [loadMyInvites]);
  const setMemberRole: Ctx["setMemberRole"] = useCallback(async (userId, role) => {
    const res = await patchJSON(`/api/v1/members/${userId}?fleet=${fleet}`, { role });
    if (!res.ok) { await fail(res, "Role change failed"); return; }
    loadMembers(fleet);
  }, [fleet, loadMembers]);
  const removeFleetMember: Ctx["removeFleetMember"] = useCallback(async (userId) => {
    const res = await del(`/api/v1/members/${userId}?fleet=${fleet}`);
    if (!res.ok) { await fail(res, "Remove failed"); return; }
    loadMembers(fleet);
  }, [fleet, loadMembers]);

  const value: Ctx = {
    user, fleets, fleet, switchFleet, createFleet, updateFleet, signOut,
    missions, missionCounts, pilots, crews, labels, rovers, enrollmentCodes, signals, newEnrollmentCode, boardTick,
    members, myRole, fleetInvites, myInvites,
    selectedOperation, openOperation, operationDetail, selectedRun, setSelectedRun, runDetail,
    createOperation, setOperationTags, setPriority, setDates, setMainOperation, setArchived,
    createLabel, deleteLabel, attachLabel, detachLabel, addPullRequest, deletePullRequest, addRelation, removeRelation, searchOperations, react,
    reassign, runOperation, cancelRun, moveOperation, addComment,
    addCrew, renameCrew, delCrew, addMission, updateMission, addMember, removeMember,
    createEnrollmentCode, revokeRover, renameRover, setRoverTags, revokeEnrollmentCode, openSignal, archiveSignal,
    invite, revokeInvite, acceptInvite, declineInvite, setMemberRole, removeFleetMember,
  };
  return <AppCtx.Provider value={value}>{children}</AppCtx.Provider>;
}
