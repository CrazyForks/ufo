"use client";

import { Mail } from "lucide-react";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { useT, type MessageKey } from "@/lib/i18n";

function roleLabel(t: (key: MessageKey) => string, role: string) {
  const key = `members.role.${role}` as MessageKey;
  return key === "members.role.owner" || key === "members.role.admin" || key === "members.role.member" ? t(key) : role;
}

// Pending invitations addressed to the signed-in user (matched by email).
export function InviteBanner() {
  const app = useApp();
  const t = useT();
  if (app.myInvites.length === 0) return null;
  return (
    <div className="space-y-1 border-b border-border bg-brand/5 px-4 py-2">
      {app.myInvites.map((inv) => (
        <div key={inv.id} className="flex items-center gap-2 text-sm">
          <Mail className="size-4 text-brand" />
          <span>{t("invite.youreInvited", { fleet: inv.fleet_name, role: roleLabel(t, inv.role) })}</span>
          <Button size="sm" className="ml-auto" onClick={() => app.acceptInvite(inv.id, inv.fleet_id)}>{t("invite.accept")}</Button>
          <Button size="sm" variant="ghost" onClick={() => app.declineInvite(inv.id)}>{t("invite.decline")}</Button>
        </div>
      ))}
    </div>
  );
}
