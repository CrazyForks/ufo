"use client";

import { Users } from "lucide-react";
import { PilotIcon } from "@/components/pilot-icon";
import { useT } from "@/lib/i18n";
import { pilotLabel } from "@/lib/labels";
import type { Crew } from "@/lib/types";

export function CrewOption({ crew, crewIcon = "icon" }: { crew: Crew; crewIcon?: "emoji" | "icon" }) {
  const t = useT();
  const pilots = (crew.members ?? []).filter((m) => m.member_type === "pilot").sort((a, b) => Number(b.role === "captain") - Number(a.role === "captain") || pilotLabel(a.member_id).localeCompare(pilotLabel(b.member_id)));
  const visible = pilots.slice(0, 4);
  return (
    <span className="flex min-w-0 items-center gap-2">
      {crewIcon === "emoji" ? <span className="min-w-0 truncate">👥 {crew.name}</span> : (
        <>
          <Users className="size-3.5 shrink-0" />
          <span className="min-w-0 truncate">{crew.name}</span>
        </>
      )}
      {visible.length > 0 && (
        <span title={pilots.map((m) => `${pilotLabel(m.member_id)}${m.role === "captain" ? t("assignee.captainSuffix") : ""}`).join(", ")} className="ml-auto inline-flex shrink-0 items-center gap-0.5">
          {visible.map((m) => <PilotIcon key={m.member_id} kind={m.member_id} size={12} />)}
          {pilots.length > visible.length && <span className="text-[10px] text-muted-foreground">+{pilots.length - visible.length}</span>}
        </span>
      )}
    </span>
  );
}

export function PilotOption({ kind, unavailable }: { kind: string; unavailable: boolean }) {
  const t = useT();
  return <span className="flex items-center gap-2"><PilotIcon kind={kind} /> {pilotLabel(kind)}{unavailable && t("assignee.noRover")}</span>;
}
