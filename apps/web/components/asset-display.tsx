"use client";

import { File, FileArchive, FileAudio, FileCode, FileImage, FileJson, FileSpreadsheet, FileText, FileType, FileVideo, Link2, Loader2, Presentation, Sparkles, Trash2, UserRound } from "lucide-react";
import { bundledLanguagesInfo } from "shiki";
import { assetFilePath } from "@/lib/assets";
import { t } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import type { Asset } from "@/lib/types";

export type AssetSource = "upload" | "output";

function assetSourceLabel(source: AssetSource): string {
  return source === "output" ? t("asset.output") : t("asset.upload");
}

export function assetSource(asset: Asset): AssetSource {
  return typeof asset.metadata?.run_id === "string" ? "output" : "upload";
}

export function AssetSourceIcon({ asset, className }: { asset: Asset; className?: string }) {
  const source = assetSource(asset);
  const Icon = source === "output" ? Sparkles : UserRound;
  const label = assetSourceLabel(source);
  return (
    <span className={cn("inline-flex shrink-0 items-center text-muted-foreground", className)} title={label} aria-label={label}>
      <Icon className="size-3" />
    </span>
  );
}

export function AssetKindIcon({ asset, className = "size-4 shrink-0 text-muted-foreground" }: { asset: Asset; className?: string }) {
  if (isImageAsset(asset)) return <FileImage className={className} />;
  if (isAudioAsset(asset)) return <FileAudio className={className} />;
  if (isVideoAsset(asset)) return <FileVideo className={className} />;
  if (isArchiveAsset(asset)) return <FileArchive className={className} />;
  if (isSpreadsheetAsset(asset)) return <FileSpreadsheet className={className} />;
  if (isJsonAsset(asset)) return <FileJson className={className} />;
  if (isCodeAsset(asset)) return <FileCode className={className} />;
  if (isPresentationAsset(asset)) return <Presentation className={className} />;
  if (isMarkdownAsset(asset)) return <FileType className={className} />;
  if (isPdfAsset(asset) || isTextAsset(asset) || isWordAsset(asset)) return <FileText className={className} />;
  return <File className={className} />;
}

export function AssetChipStrip({ assets, deletingAssetId, onInsert, onDelete, className }: { assets: Asset[]; deletingAssetId?: string | null; onInsert?: (asset: Asset) => void; onDelete?: (asset: Asset) => void; className?: string }) {
  if (assets.length === 0) return null;
  return (
    <div className={cn("flex flex-wrap gap-1.5", className)}>
      {assets.map((asset) => {
        const deleting = deletingAssetId === asset.id;
        const insertLabel = t("asset.insert", { name: asset.filename });
        const deleteLabel = t("op.deleteAsset", { name: asset.filename });
        const content = (
          <>
            <AssetKindIcon asset={asset} className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0 max-w-40 truncate">{asset.filename}</span>
            <span className="shrink-0 tabular-nums text-muted-foreground">{formatBytes(asset.byte_size)}</span>
            {onInsert && !onDelete && <Link2 className="size-3 shrink-0 text-muted-foreground" />}
          </>
        );
        const chipClass = "inline-flex h-7 max-w-full items-center gap-1.5 rounded-md border border-border bg-muted/30 px-2 text-xs";
        if (onInsert && !onDelete) {
          return (
            <button key={asset.id} type="button" className={cn(chipClass, "hover:bg-accent hover:text-accent-foreground")} title={insertLabel} onClick={() => onInsert(asset)}>
              {content}
            </button>
          );
        }
        return (
          <span key={asset.id} className={chipClass}>
            {content}
            {onInsert && (
              <button type="button" className="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground" title={insertLabel} aria-label={insertLabel} onClick={() => onInsert(asset)}>
                <Link2 className="size-3" />
              </button>
            )}
            {onDelete && (
              <button type="button" className="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-destructive disabled:opacity-50" title={deleteLabel} aria-label={deleteLabel} disabled={deleting} onClick={() => onDelete(asset)}>
                {deleting ? <Loader2 className="size-3 animate-spin" /> : <Trash2 className="size-3" />}
              </button>
            )}
          </span>
        );
      })}
    </div>
  );
}

export function assetInlineContentURL(asset: Asset) {
  const url = asset.url || assetFilePath(asset.id);
  return `${url}${url.includes("?") ? "&" : "?"}disposition=inline`;
}

export function assetExtension(asset: Asset) {
  const i = asset.filename.lastIndexOf(".");
  return i >= 0 ? asset.filename.slice(i).toLowerCase() : "";
}

function assetBasename(asset: Asset) {
  return asset.filename.toLowerCase().split(/[\\/]/).pop() ?? "";
}

const SHIKI_LANGUAGE_BY_NAME = new Map(bundledLanguagesInfo.flatMap((language) => [language.id, ...(language.aliases ?? [])].map((name) => [name.toLowerCase(), language.id])));

export function isImageAsset(asset: Asset) {
  return asset.content_type.toLowerCase().startsWith("image/") || [
    ".avif", ".bmp", ".gif", ".ico", ".jpeg", ".jpg", ".png", ".svg", ".webp",
  ].includes(assetExtension(asset));
}

export function isAudioAsset(asset: Asset) {
  return asset.content_type.toLowerCase().startsWith("audio/");
}

export function isVideoAsset(asset: Asset) {
  return asset.content_type.toLowerCase().startsWith("video/");
}

export function isPdfAsset(asset: Asset) {
  return asset.content_type.toLowerCase() === "application/pdf" || assetExtension(asset) === ".pdf";
}

export function isTextAsset(asset: Asset) {
  const contentType = asset.content_type.toLowerCase();
  const ext = assetExtension(asset);
  return contentType.startsWith("text/") || [
    ".cfg", ".conf", ".csv", ".diff", ".env", ".ini", ".log", ".md", ".patch", ".toml", ".txt", ".xml", ".yaml", ".yml",
  ].includes(ext);
}

export function isMarkdownAsset(asset: Asset) {
  const contentType = asset.content_type.toLowerCase();
  return contentType === "text/markdown" || [".md", ".markdown"].includes(assetExtension(asset));
}

export function isJsonAsset(asset: Asset) {
  const contentType = asset.content_type.toLowerCase();
  return contentType === "application/json" || contentType.endsWith("+json") || assetExtension(asset) === ".json";
}

export function canRenderTextAsset(asset: Asset) {
  return isMarkdownAsset(asset) || isJsonAsset(asset) || isCodeAsset(asset);
}

export function isCodeAsset(asset: Asset) {
  return !isMarkdownAsset(asset) && !isJsonAsset(asset) && assetCodeLanguage(asset) != null;
}

export function assetCodeLanguage(asset: Asset) {
  const ext = assetExtension(asset).slice(1);
  const name = assetBasename(asset);
  return SHIKI_LANGUAGE_BY_NAME.get(ext) ?? SHIKI_LANGUAGE_BY_NAME.get(name) ?? null;
}

export function isArchiveAsset(asset: Asset) {
  const contentType = asset.content_type.toLowerCase();
  return contentType === "application/zip" || contentType === "application/x-tar" || contentType === "application/gzip" || [".7z", ".gz", ".rar", ".tar", ".tgz", ".zip"].includes(assetExtension(asset));
}

export function isWordAsset(asset: Asset) {
  const contentType = asset.content_type.toLowerCase();
  return contentType === "application/msword" || contentType === "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || [".doc", ".docx"].includes(assetExtension(asset));
}

export function isPresentationAsset(asset: Asset) {
  const contentType = asset.content_type.toLowerCase();
  return contentType === "application/vnd.ms-powerpoint" || contentType === "application/vnd.openxmlformats-officedocument.presentationml.presentation" || [".ppt", ".pptx"].includes(assetExtension(asset));
}

export function isSpreadsheetAsset(asset: Asset) {
  const contentType = asset.content_type.toLowerCase();
  return contentType === "application/vnd.ms-excel" || contentType === "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || [".xls", ".xlsx"].includes(assetExtension(asset));
}

export function canPreviewAsset(asset: Asset) {
  return isImageAsset(asset) || isPdfAsset(asset) || isTextAsset(asset) || isJsonAsset(asset) || isCodeAsset(asset);
}

export function assetKindLabel(asset: Asset) {
  const contentType = asset.content_type.toLowerCase();
  if (contentType.startsWith("image/")) return t("asset.image");
  if (contentType.startsWith("audio/")) return t("asset.audio");
  if (contentType.startsWith("video/")) return t("asset.video");
  if (contentType === "application/pdf") return t("asset.pdf");
  if (isArchiveAsset(asset)) return t("asset.archive");
  if (isWordAsset(asset)) return t("asset.word");
  if (isPresentationAsset(asset)) return t("asset.powerpoint");
  if (isSpreadsheetAsset(asset)) return t("asset.spreadsheet");
  if (isJsonAsset(asset)) return t("asset.json");
  if (isCodeAsset(asset)) return t("asset.code");
  if (isMarkdownAsset(asset)) return t("asset.markdown");
  if (isTextAsset(asset)) return t("asset.text");
  if (contentType === "application/octet-stream") return t("asset.binary");
  return contentType || t("common.file");
}

export function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

export function formatAssetDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
