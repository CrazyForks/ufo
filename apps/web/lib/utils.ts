import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function hideFlowControlFlags(text: string) {
  return text
    .replace(/^[ \t]*@@UFO_STATUS:(?:in_review|done|blocked|cancelled)(?:@@)?[ \t]*(?:\r?\n|$)/gim, "")
    .replace(/^[ \t]*@@UFO_SUB_OPERATIONS@@.*(?:\r?\n|$)/gim, "")
    .replace(/@@UFO_NEEDS_INPUT@@\s*/g, "")
    .replace(/@@UFO_STATUS:(?:in_review|done|blocked|cancelled)(?:@@)?/gi, "")
    .replace(/@@UFO_SUB_OPERATIONS@@/g, "");
}
