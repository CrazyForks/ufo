import type { Asset } from "@/lib/types";

export function assetFilePath(assetId: string) {
  return `/v1/assets/${assetId}/file`;
}

export function assetMarkdownLink(asset: Asset) {
  const label = asset.filename.replace(/[[\]\\]/g, "\\$&");
  return `[${label}](${asset.url || assetFilePath(asset.id)})`;
}

export function appendAssetLink(text: string, asset: Asset) {
  const link = assetMarkdownLink(asset);
  return text.trim() ? `${text.replace(/\s+$/, "")}\n${link}` : link;
}

export function removeAssetLink(text: string, asset: Asset) {
  const link = assetMarkdownLink(asset);
  return text.split("\n").filter((line) => line.trim() !== link).join("\n");
}
