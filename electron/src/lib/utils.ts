import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { Stream } from "$lib/types/addons";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type WithoutChild<T> = T extends { child?: any } ? Omit<T, "child"> : T;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type WithoutChildren<T> = T extends { children?: any }
  ? Omit<T, "children">
  : T;
export type WithoutChildrenOrChild<T> = WithoutChildren<WithoutChild<T>>;
export type WithElementRef<T, U extends HTMLElement = HTMLElement> = T & {
  ref?: U | null;
};

export function countryName(code: string): string {
  try {
    return new Intl.DisplayNames(["en"], { type: "region" }).of(code) ?? code;
  } catch {
    return code;
  }
}

const qualityOrder = [
  "4k dv",
  "4k hdr",
  "4k",
  "1080p",
  "720p",
  "480p",
  "ts",
  "cam",
];

export function inferQuality(stream: Stream): string | null {
  const qualityLine = stream.name.split("\n")[1]?.trim().toLowerCase();
  if (qualityLine) {
    // Extract the best known quality from the line rather than returning it raw
    for (const q of qualityOrder) {
      if (qualityLine.includes(q)) return q;
    }
  }

  const text = `${stream.name} ${stream.title}`.toLowerCase();
  if (text.includes("dolby vision") || text.includes("4k dv")) return "4k dv";
  if (text.includes("hdr")) return "4k hdr";
  if (text.includes("2160") || text.includes("4k")) return "4k";
  if (text.includes("1080")) return "1080p";
  if (text.includes("720")) return "720p";
  if (text.includes("480")) return "480p";
  if (
    text.includes("telesync") ||
    text.includes("ts ") ||
    text.includes("[ts]")
  )
    return "ts";
  if (text.includes("hdcam") || text.includes("cam")) return "cam";
  return null;
}

export function getMaxQuality(streams: Stream[]): string | null {
  const qualities = streams.map(inferQuality).filter(Boolean) as string[];
  return qualityOrder.find((q) => qualities.includes(q)) ?? null;
}

export function qualityClass(quality: string): string {
  if (quality.includes("dv"))
    return "border-purple-500/40 bg-purple-500/35 text-purple-400";
  if (quality.includes("hdr"))
    return "border-blue-500/40 bg-blue-500/35 text-blue-400";
  if (quality === "4k")
    return "border-cyan-500/40 bg-cyan-500/35 text-cyan-400";
  if (quality === "1080p")
    return "border-green-500/40 bg-green-500/35 text-green-400";
  if (quality === "720p")
    return "border-yellow-500/40 bg-yellow-500/35 text-yellow-400";
  if (quality === "480p")
    return "border-orange-500/40 bg-orange-500/35 text-orange-400";
  if (quality === "ts") return "border-red-500/40 bg-red-500/35 text-red-400";
  if (quality === "cam") return "border-red-700/40 bg-red-700/35 text-red-500";
  return "border-border bg-secondary text-secondary-foreground";
}
