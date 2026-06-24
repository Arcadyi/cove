import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { Stream } from "$lib/types/addons";
import {
  Details,
  MediaImages,
  MediaVideoObject,
  MediaVideos,
} from "$lib/types/tmdb";
import { SvelteMap } from "svelte/reactivity";
import type { WatchProgress } from "$lib/types/library";

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

export function formatRuntime(d: Details): string {
  if (d.runtime > 0) return `${Math.floor(d.runtime / 60)}h ${d.runtime % 60}m`;
  if (d.episode_run_time?.[0]) return `${d.episode_run_time[0]}m / ep`;
  // TMDB often leaves episode_run_time empty for TV — fall back to the
  // season/episode count.
  if (d.number_of_seasons > 0)
    return `${d.number_of_seasons} Season${d.number_of_seasons === 1 ? "" : "s"}`;
  if (d.number_of_episodes > 0)
    return `${d.number_of_episodes} Episode${d.number_of_episodes === 1 ? "" : "s"}`;
  return "";
}

export function formatRating(d: Details): string {
  for (const r of d.release_dates?.results ?? []) {
    if (r.iso_3166_1 === "US") {
      for (const rd of r.release_dates ?? []) {
        if (rd.certification) return rd.certification;
      }
    }
  }
  for (const r of d.content_ratings?.results ?? []) {
    if (r.iso_3166_1 === "US" && r.rating) return r.rating;
  }
  return "";
}

interface ImageOptions {
  aspect_ratio?: number;
  height?: number;
  iso?: string;
  voteAverage?: number;
  voteCount?: number;
  minWidth?: number;
  randomize?: boolean;
}

export function getImageOpt(
  images: MediaImages | undefined,
  type: "backdrops" | "logos" | "posters",
  opts: ImageOptions = {},
): string {
  if (!images || !images[type] || images[type].length === 0) return "";

  const list = images[type];

  // 1. Filter all images that meet the criteria
  const matches = list.filter((img) => {
    if (opts.iso && img.iso_639_1 !== null && img.iso_639_1 !== opts.iso)
      return false;
    if (opts.height !== undefined && img.height !== opts.height) return false;
    if (
      opts.aspect_ratio !== undefined &&
      Math.abs(img.aspect_ratio - opts.aspect_ratio) > 0.1
    )
      return false;
    if (opts.voteAverage !== undefined && img.vote_average < opts.voteAverage)
      return false;
    if (opts.voteCount !== undefined && img.vote_count < opts.voteCount)
      return false;
    return !(opts.minWidth !== undefined && img.width < opts.minWidth);
  });

  // 2. Handle the selection
  if (matches.length > 0) {
    if (opts.randomize) {
      const randomIndex = Math.floor(Math.random() * matches.length);
      return matches[randomIndex].url;
    }
    return matches[0].url;
  }

  // 3. Fallback: Return the first available image if nothing matches criteria
  return list[0]?.url ?? "";
}

interface VideoOptions {
  iso?: string;
  site?: string;
  size?: number;
  official?: boolean;
  randomize?: boolean;
}

function buildEmbedUrl(video: MediaVideoObject): string {
  if (video.embed_url) return video.embed_url;

  const site = video.site.toLowerCase();
  if (site === "youtube") {
    return `https://www.youtube.com/embed/${video.key}`;
  }
  if (site === "vimeo") {
    return `https://player.vimeo.com/video/${video.key}`;
  }
  return "";
}

export function getVideoOpt(
  videos: MediaVideos | undefined | null,
  type: "Clip" | "Featurette" | "Behind the Scenes" | "Teaser" | "Trailer",
  opts: VideoOptions = {},
): string {
  if (!videos || !Array.isArray(videos.results)) return "";

  let list = videos.results.filter((vid) => vid.type === type);

  if (list.length === 0 && type !== "Trailer") {
    list = videos.results.filter((vid) => vid.type === "Trailer");
  }

  if (list.length === 0) {
    return videos.results[0] ? buildEmbedUrl(videos.results[0]) : "";
  }

  const matches = list.filter((vid) => {
    if (opts.iso && vid.iso_639_1 && vid.iso_639_1 !== opts.iso) return false;
    if (opts.site && vid.site && vid.site !== opts.site) return false;
    if (opts.size !== undefined && vid.size !== opts.size) return false;
    return !(opts.official !== undefined && vid.official !== opts.official);
  });

  if (matches.length > 0) {
    if (opts.randomize) {
      const randomIndex = Math.floor(Math.random() * matches.length);
      return buildEmbedUrl(matches[randomIndex]);
    }
    return buildEmbedUrl(matches[0]);
  }

  return buildEmbedUrl(list[0]);
}

export function relativeDate(dateStr: string): string {
  const days = Math.ceil(
    (new Date(dateStr).getTime() - Date.now()) / (1000 * 60 * 60 * 24),
  );
  if (days <= 1) return "Coming Tomorrow";
  if (days <= 7) return `Coming in ${days} Days`;
  if (days <= 14) return "Coming Next Week";
  return `Coming ${new Date(dateStr).toLocaleDateString(undefined, { month: "short", day: "numeric" })}`;
}

export function epKey(season: number, episode: number): string {
  return `${season}:${episode}`;
}

export function epProgress(
  season: number,
  episode: number,
  progressMap: SvelteMap<string, WatchProgress>,
): WatchProgress | undefined {
  return progressMap.get(epKey(season, episode));
}

export function progressPct(p: WatchProgress): number {
  if (!p.duration_seconds) return 0;
  return Math.min(100, (p.position_seconds / p.duration_seconds) * 100);
}
