"use client";

import { Loader2, Trash2 } from "lucide-react";
import { AssetKindIcon, formatBytes } from "@/components/asset-display";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useT } from "@/lib/i18n";
import type { Asset } from "@/lib/types";

export function AssetDeleteDialog({
  asset,
  open,
  deleting,
  error,
  onOpenChange,
  onConfirm,
}: {
  asset: Asset | null;
  open: boolean;
  deleting: boolean;
  error?: string | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: (asset: Asset) => void;
}) {
  const t = useT();
  if (!asset) return null;
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!deleting) onOpenChange(next); }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{t("asset.deleteTitle")}?</DialogTitle>
          <DialogDescription>{t("asset.deleteDescription")}</DialogDescription>
        </DialogHeader>
        <div className="flex min-w-0 items-center gap-3 rounded-md border border-border bg-muted/30 p-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-background text-muted-foreground">
            <AssetKindIcon asset={asset} className="size-4" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">{asset.filename}</div>
            <div className="text-xs tabular-nums text-muted-foreground">{formatBytes(asset.byte_size)}</div>
          </div>
        </div>
        {error && <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">{error}</div>}
        <div className="mt-4 flex justify-end gap-2">
          <Button type="button" variant="ghost" disabled={deleting} onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
          <Button type="button" variant="destructive" disabled={deleting} onClick={() => onConfirm(asset)}>
            {deleting ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
            {t("asset.delete")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
