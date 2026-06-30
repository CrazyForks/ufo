"use client";

import React, { useEffect, useState } from "react";
import { Copy, Loader2 } from "lucide-react";
import { codeToHtml } from "shiki";
import { AssetKindIcon, assetCodeLanguage, assetInlineContentURL, canPreviewAsset, isCodeAsset, isImageAsset, isJsonAsset, isMarkdownAsset, isPdfAsset } from "@/components/asset-display";
import { SelectionActionsMenu, copyText, selectedTextWithin } from "@/components/selection-actions-menu";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api";
import { cn } from "@/lib/utils";
import type { Asset } from "@/lib/types";

type AssetPreviewSize = "panel" | "dialog";

export function AssetPreview({
  asset,
  size = "panel",
  renderMarkdown,
}: {
  asset: Asset;
  size?: AssetPreviewSize;
  renderMarkdown?: (text: string) => React.ReactNode;
}) {
  const url = assetInlineContentURL(asset);
  if (isImageAsset(asset)) {
    return <img src={url} alt={asset.filename} className={cn("w-full object-contain bg-muted/30", size === "dialog" ? "max-h-[75vh]" : "max-h-80")} />;
  }
  if (isPdfAsset(asset)) {
    return <iframe src={url} title={asset.filename} className={cn("w-full bg-background", size === "dialog" ? "h-[75vh]" : "h-80")} />;
  }
  if (canPreviewAsset(asset)) {
    return <AssetTextPreview asset={asset} size={size} renderMarkdown={renderMarkdown} />;
  }
  return (
    <div className={cn("flex items-center justify-center gap-2 text-xs text-muted-foreground", size === "dialog" ? "h-80" : "h-32")}>
      <AssetKindIcon asset={asset} className="size-4" />
      Preview unavailable
    </div>
  );
}

function AssetTextPreview({ asset, size, renderMarkdown }: { asset: Asset; size: AssetPreviewSize; renderMarkdown?: (text: string) => React.ReactNode }) {
  const [text, setText] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const rootRef = React.useRef<HTMLDivElement>(null);
  const selectionMenuRef = React.useRef<HTMLDivElement>(null);
  const selectedTextRef = React.useRef("");
  const url = assetInlineContentURL(asset);
  const maxHeight = size === "dialog" ? "max-h-[75vh]" : "max-h-80";
  useEffect(() => {
    let active = true;
    setText(null);
    setFailed(false);
    apiFetch(url)
      .then(async (res) => {
        if (!res.ok) throw new Error(`preview failed: ${res.status}`);
        return res.text();
      })
      .then((body) => {
        if (active) setText(body);
      })
      .catch(() => {
        if (active) setFailed(true);
      });
    return () => { active = false; };
  }, [asset.id, url]);
  function hideSelectionMenu() {
    selectedTextRef.current = "";
    if (selectionMenuRef.current) selectionMenuRef.current.hidden = true;
  }
  function clearSelection() {
    hideSelectionMenu();
    window.getSelection()?.removeAllRanges();
  }
  function updateSelectionMenu() {
    const root = rootRef.current;
    const menu = selectionMenuRef.current;
    const selection = window.getSelection();
    const selectedText = selectedTextWithin(root);
    if (!root || !menu || !selection || !selectedText || selection.rangeCount === 0) return hideSelectionMenu();
    const range = selection.getRangeAt(0);
    const rects = range.getClientRects();
    const rect = rects.length ? rects[rects.length - 1] : range.getBoundingClientRect();
    const rootRect = root.getBoundingClientRect();
    selectedTextRef.current = selectedText;
    menu.style.left = `${Math.max(0, Math.min(rect.right - rootRect.left - 28, rootRect.width - 28))}px`;
    menu.style.top = `${Math.max(0, rect.top - rootRect.top - 34)}px`;
    menu.hidden = false;
  }
  function copySelection() {
    const selectedText = selectedTextRef.current;
    if (!selectedText) return;
    void copyText(selectedText);
    clearSelection();
  }

  if (failed) {
    return <div className="flex h-32 items-center justify-center text-xs text-muted-foreground">Preview unavailable</div>;
  }
  if (text == null) {
    return (
      <div className="flex h-32 items-center justify-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" />
        Loading preview...
      </div>
    );
  }
  return (
    <div ref={rootRef} className="relative" onMouseUp={updateSelectionMenu} onKeyUp={updateSelectionMenu}>
      <SelectionActionsMenu menuRef={selectionMenuRef} onCopy={copySelection} />
      {isMarkdownAsset(asset) && renderMarkdown ? (
        <div className={cn(maxHeight, "overflow-auto p-3")}>
          {renderMarkdown(text)}
        </div>
      ) : isJsonAsset(asset) || isCodeAsset(asset) ? (
        <CodePreview language={isJsonAsset(asset) ? "json" : assetCodeLanguage(asset) ?? "text"} text={formatTextPreview(asset, text)} maxHeight={maxHeight} />
      ) : (
        <pre className={cn(maxHeight, "overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-xs leading-relaxed text-foreground")}>
          {formatTextPreview(asset, text)}
        </pre>
      )}
    </div>
  );
}

function CodePreview({ language, text, maxHeight }: { language: string; text: string; maxHeight: string }) {
  const [html, setHtml] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    setHtml(null);
    codeToHtml(text, { lang: language, themes: { light: "light-plus", dark: "dark-plus" } })
      .then((next) => { if (active) setHtml(next); })
      .catch(() => { if (active) setHtml(null); });
    return () => { active = false; };
  }, [language, text]);
  if (!html) {
    return (
      <pre className={cn(maxHeight, "overflow-auto bg-muted/30 p-3 font-mono text-xs leading-relaxed text-foreground")}>
        <code>{text}</code>
      </pre>
    );
  }
  return <div className={cn(maxHeight, "overflow-auto bg-muted/30 font-mono text-xs leading-relaxed [&_pre]:!m-0 [&_pre]:!bg-transparent [&_pre]:p-3")} dangerouslySetInnerHTML={{ __html: html }} />;
}

function formatTextPreview(asset: Asset, text: string) {
  if (!isJsonAsset(asset)) return text;
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

export function AssetTextCopyButton({ asset, className }: { asset: Asset; className?: string }) {
  if (!canPreviewAsset(asset) || isImageAsset(asset) || isPdfAsset(asset)) return null;
  async function copyAssetText() {
    const res = await apiFetch(assetInlineContentURL(asset));
    if (!res.ok) return;
    await copyText(await res.text());
  }
  return (
    <Button variant="ghost" size="icon-sm" className={cn("size-6 shrink-0 text-muted-foreground", className)} title="Copy text" aria-label={`Copy text from ${asset.filename}`} onClick={() => void copyAssetText()}>
      <Copy className="size-3.5" />
    </Button>
  );
}
