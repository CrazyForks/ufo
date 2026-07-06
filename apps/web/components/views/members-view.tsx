"use client";

import { useState } from "react";
import { Link2, Trash2, UserPlus } from "lucide-react";
import { toast } from "sonner";
import { useApp } from "@/components/app-provider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { useT, type MessageKey } from "@/lib/i18n";
import { initials } from "@/lib/labels";
import { SECTION_ICONS } from "@/lib/section-icons";
import { validAuthEmail } from "@/lib/auth";

function roleLabel(t: (key: MessageKey) => string, role: string) {
  const key = `members.role.${role}` as MessageKey;
  return key === "members.role.owner" || key === "members.role.admin" || key === "members.role.member" ? t(key) : role;
}

function inviteAuthHref(email: string): string {
  const params = new URLSearchParams();
  const trimmed = email.trim().toLowerCase();
  if (trimmed) params.set("email", trimmed);
  const q = params.toString();
  return q ? `/signup?${q}` : "/signup";
}

async function copyInviteLink(email: string, copied: string, failed: string) {
  const url = `${window.location.origin}${inviteAuthHref(email)}`;
  try {
    await navigator.clipboard.writeText(url);
    toast.success(copied);
  } catch {
    toast.error(failed);
  }
}

export function MembersView() {
  const app = useApp();
  const t = useT();
  const personal = app.fleets.find((f) => f.id === app.fleet)?.kind === "personal";
  const canAdministerFleet = (app.myRole === "owner" || app.myRole === "admin") && !personal;
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("member");

  async function sendInvite(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = email.trim().toLowerCase();
    if (!trimmed) return;
    if (!validAuthEmail(trimmed)) {
      toast.error(t("auth.invalidEmail"));
      return;
    }
    if (await app.invite(trimmed, role)) {
      setEmail("");
      await copyInviteLink(trimmed, t("members.linkCopied"), t("members.linkCopyFailed"));
    }
  }

  return (
    <div className="h-full min-h-0 overflow-y-auto">
    <div className="mx-auto max-w-3xl space-y-4 p-4 pb-8">
      <Card>
        <CardHeader><CardTitle className="flex items-center gap-2 text-base"><SECTION_ICONS.members className="size-4" /> {t("members.title")}</CardTitle></CardHeader>
        <CardContent className="space-y-1">
          {app.members.map((m) => (
            <div key={m.id} className="flex items-center gap-3 py-2">
              <button type="button" className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 text-left" onClick={() => app.openUser(m.id)}>
                <Avatar className="size-7"><AvatarFallback>{initials(m.name || m.email)}</AvatarFallback></Avatar>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium hover:underline">{m.name || m.email}{m.id === app.user.id && t("members.youSuffix")}</p>
                  <p className="truncate text-xs text-muted-foreground">{m.email}</p>
                </div>
              </button>
              {app.myRole === "owner" && m.id !== app.user.id ? (
                <Select value={m.role} onValueChange={(v) => app.setMemberRole(m.id, v)}>
                  <SelectTrigger className="h-7 w-28 text-xs"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="owner">{t("members.role.owner")}</SelectItem>
                    <SelectItem value="admin">{t("members.role.admin")}</SelectItem>
                    <SelectItem value="member">{t("members.role.member")}</SelectItem>
                  </SelectContent>
                </Select>
              ) : (
                <Badge variant="secondary">{roleLabel(t, m.role)}</Badge>
              )}
              {canAdministerFleet && m.role !== "owner" && m.id !== app.user.id && (
                <Button variant="ghost" size="icon-sm" onClick={() => app.removeFleetMember(m.id)}><Trash2 /></Button>
              )}
            </div>
          ))}
        </CardContent>
      </Card>

      {personal && (
        <p className="text-sm text-muted-foreground">{t("members.personalFleet")}</p>
      )}

      {canAdministerFleet && (
        <Card>
          <CardHeader><CardTitle className="flex items-center gap-2 text-base"><UserPlus className="size-4" /> {t("members.invite")}</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            <form className="flex gap-2" onSubmit={sendInvite}>
              <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder={t("members.emailPlaceholder")} className="flex-1" />
              <Select value={role} onValueChange={setRole}>
                <SelectTrigger className="w-28"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">{t("members.role.member")}</SelectItem>
                  <SelectItem value="admin">{t("members.role.admin")}</SelectItem>
                </SelectContent>
              </Select>
              <Button type="submit">{t("members.invite")}</Button>
            </form>
            <p className="text-xs text-muted-foreground">{t("members.inviteHint")}</p>
            {app.fleetInvites.length > 0 && (
              <div className="divide-y divide-border border-t border-border pt-2">
                {app.fleetInvites.map((inv) => (
                  <div key={inv.id} className="flex items-center justify-between gap-2 py-2 text-sm">
                    <span className="min-w-0 truncate">{inv.invitee_email} <Badge variant="secondary">{roleLabel(t, inv.role)}</Badge> <span className="text-xs text-muted-foreground">{t("members.pending")}</span></span>
                    <div className="flex shrink-0 items-center gap-1">
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => copyInviteLink(inv.invitee_email, t("members.linkCopied"), t("members.linkCopyFailed"))}
                      >
                        <Link2 className="size-3.5" />
                        {t("members.copyLink")}
                      </Button>
                      <Button variant="ghost" size="sm" className="text-destructive" onClick={() => app.revokeInvite(inv.id)}>{t("members.revoke")}</Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
    </div>
  );
}
