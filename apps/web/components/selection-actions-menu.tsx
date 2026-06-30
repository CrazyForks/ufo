"use client";

import type React from "react";
import { Copy, Quote } from "lucide-react";
import { Button } from "@/components/ui/button";

export function selectedTextWithin(el: HTMLElement | null) {
  const selection = window.getSelection();
  if (!el || !selection || selection.isCollapsed || !selection.anchorNode || !selection.focusNode) return "";
  if (!el.contains(selection.anchorNode) || !el.contains(selection.focusNode)) return "";
  return selection.toString().trim();
}

export async function copyText(text: string) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {}
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.style.cssText = "position:fixed;left:-9999px;top:0";
  document.body.appendChild(textarea);
  textarea.select();
  const ok = document.execCommand("copy");
  textarea.remove();
  return ok;
}

export function SelectionActionsMenu({
  menuRef,
  onCopy,
  onQuote,
}: {
  menuRef: React.RefObject<HTMLDivElement | null>;
  onCopy: () => void;
  onQuote?: () => void;
}) {
  function run(event: React.MouseEvent, action?: () => void) {
    event.preventDefault();
    event.stopPropagation();
    action?.();
  }
  return (
    <div ref={menuRef} hidden className="absolute z-20 flex overflow-hidden rounded-full border border-border bg-popover text-muted-foreground shadow-md">
      <Button
        variant="ghost"
        size="icon-sm"
        tabIndex={-1}
        className="size-7 rounded-none hover:bg-accent hover:text-accent-foreground"
        title="Copy selection"
        aria-label="Copy selection"
        onMouseDown={(event) => run(event, onCopy)}
        onMouseUp={(event) => event.stopPropagation()}
        onClick={(event) => run(event)}
      >
        <Copy className="size-3.5" />
      </Button>
      {onQuote && (
        <Button
          variant="ghost"
          size="icon-sm"
          tabIndex={-1}
          className="size-7 rounded-none border-l border-border hover:bg-accent hover:text-accent-foreground"
          title="Quote selection"
          aria-label="Quote selection"
          onMouseDown={(event) => run(event, onQuote)}
          onMouseUp={(event) => event.stopPropagation()}
          onClick={(event) => run(event)}
        >
          <Quote className="size-3.5" />
        </Button>
      )}
    </div>
  );
}
