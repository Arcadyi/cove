// $lib/streamSelection.ts
//
// Shared logic for parsing and ranking addon streams. Used by StreamsList for
// manual sort/filter, and by the "auto-select best stream" feature to pick a
// winner without the user touching the list at all.

import type { Stream } from "$lib/types/addons";
import { inferQuality } from "$lib/utils";

export type StreamSelectionMode =
  | "balanced"
  | "seeders"
  | "quality"
  | "smallest"
  | "bandwidth";

export const STREAM_SELECTION_MODES: {
  value: StreamSelectionMode;
  label: string;
  description: string;
}[] = [
  {
    value: "balanced",
    label: "Most Seeders & Lowest Size",
    description:
      "Favors well-seeded streams without picking an unnecessarily large file.",
  },
  {
    value: "seeders",
    label: "Most Seeders",
    description:
      "Prioritizes the most reliable, fastest-starting stream available.",
  },
  {
    value: "quality",
    label: "Highest Quality",
    description: "Always picks the best resolution, regardless of file size.",
  },
  {
    value: "smallest",
    label: "Smallest File Size",
    description: "Minimizes storage and bandwidth use.",
  },
  {
    value: "bandwidth",
    label: "Match My Internet Speed",
    description:
      "Picks the highest quality your measured connection speed can comfortably handle.",
  },
];

const QUALITY_RANK: Record<string, number> = {
  "4k dv": 7,
  "4k hdr": 6,
  "4k": 5,
  "1080p": 4,
  "720p": 3,
  "480p": 2,
  ts: 1,
  cam: 0,
};

function qualityRank(q: string | null): number {
  if (!q) return -1;
  return QUALITY_RANK[q] ?? -1;
}

// ── Parsing ──────────────────────────────────────────────────────────────────
//
// Addon titles encode seeders/size as emoji-prefixed tokens, e.g.
// "👤 42  💾 2.1 GB". There's no structured field for these, so regex it is.

export function getSeeders(stream: Stream): number {
  const match = stream.title.match(/👤\s*(\d+)/);
  return match ? Number(match[1]) : 0;
}

export function getSizeBytes(stream: Stream): number {
  const match = stream.title.match(/💾\s*([\d.]+)\s*(TB|GB|MB)/i);
  if (!match) return 0;
  const value = Number(match[1]);
  switch (match[2].toUpperCase()) {
    case "TB":
      return value * 1024 ** 4;
    case "GB":
      return value * 1024 ** 3;
    case "MB":
      return value * 1024 ** 2;
    default:
      return 0;
  }
}

/** One-line summary for logging — "seeders / size / quality". */
export function formatStreamSummary(stream: Stream): string {
  const seeders = getSeeders(stream);
  const sizeBytes = getSizeBytes(stream);
  const sizeGB = sizeBytes / 1024 ** 3;
  const sizeStr =
    sizeBytes > 0
      ? sizeGB >= 1
        ? `${sizeGB.toFixed(2)} GB`
        : `${(sizeBytes / 1024 ** 2).toFixed(0)} MB`
      : "unknown size";
  const quality = inferQuality(stream) ?? "unknown quality";
  return `${seeders} seeders, ${sizeStr}, ${quality}`;
}

interface ScoredStream {
  stream: Stream;
  seeders: number;
  sizeBytes: number;
  quality: string | null;
  isPreferred: boolean;
}

function scoreCandidates(
  streams: Stream[],
  preferredProvider?: string,
): ScoredStream[] {
  return streams.map((s) => ({
    stream: s,
    seeders: getSeeders(s),
    sizeBytes: getSizeBytes(s),
    quality: inferQuality(s),
    isPreferred: !!preferredProvider && s.addonName === preferredProvider,
  }));
}

// A preferred-provider stream only wins a close call — this bonus is small
// enough that a real quality/seeder gap from another provider still wins.
const PROVIDER_BOOST = 0.15;

/** Sorts by a mode's normalized 0..1 metric (higher = better), plus a small
 * bonus for the preferred provider, falling back to `tiebreak` on ties. */
function sortByBoostedDesc(
  pool: ScoredStream[],
  normalize: (c: ScoredStream) => number,
  tiebreak: (a: ScoredStream, b: ScoredStream) => number,
): ScoredStream {
  return pool.toSorted((a, b) => {
    const boostedA = normalize(a) + (a.isPreferred ? PROVIDER_BOOST : 0);
    const boostedB = normalize(b) + (b.isPreferred ? PROVIDER_BOOST : 0);
    const diff = boostedB - boostedA;
    return diff !== 0 ? diff : tiebreak(a, b);
  })[0];
}

export interface PickBestOptions {
  /** From the in-app speed test. Undefined/0 means "never measured". */
  measuredBandwidthMbps?: number;
  /** Runtime estimate used only by "bandwidth" mode's bitrate-budget math. */
  estimatedMinutes?: number;
  /** Matched against Stream.addonName — see Settings.defaultProvider. */
  preferredProvider?: string;
}

/**
 * Picks the single best stream from a list according to the given strategy.
 * Returns null only if the input list is empty.
 */
export function pickBestStream(
  streams: Stream[],
  mode: StreamSelectionMode,
  opts: PickBestOptions = {},
): Stream | null {
  if (streams.length === 0) return null;

  const all = scoreCandidates(streams, opts.preferredProvider);
  // A zero-seeder torrent will likely never actually start downloading, so
  // exclude them from consideration — unless it's literally the only option.
  const withSeeders = all.filter((c) => c.seeders > 0);
  const pool = withSeeders.length > 0 ? withSeeders : all;

  const qualityTiebreak = (a: ScoredStream, b: ScoredStream) => {
    const qDiff = qualityRank(b.quality) - qualityRank(a.quality);
    return qDiff !== 0 ? qDiff : b.seeders - a.seeders;
  };

  switch (mode) {
    case "seeders": {
      const maxSeeders = Math.max(1, ...pool.map((c) => c.seeders));
      return sortByBoostedDesc(
        pool,
        (c) => c.seeders / maxSeeders,
        (a, b) => b.seeders - a.seeders,
      ).stream;
    }

    case "smallest": {
      // Don't let "smallest" devolve into picking a cam-quality rip just
      // because it's tiny, if a reasonable-quality option exists.
      const decent = pool.filter(
        (c) => qualityRank(c.quality) >= qualityRank("480p"),
      );
      const fromPool = decent.length > 0 ? decent : pool;
      const knownSizes = fromPool.map((c) => c.sizeBytes).filter((b) => b > 0);
      const maxSize = Math.max(1, ...knownSizes);
      return sortByBoostedDesc(
        fromPool,
        (c) => (c.sizeBytes > 0 ? 1 - c.sizeBytes / maxSize : 0.5),
        (a, b) => a.sizeBytes - b.sizeBytes,
      ).stream;
    }

    case "quality": {
      const maxRank = Math.max(1, ...pool.map((c) => qualityRank(c.quality)));
      return sortByBoostedDesc(
        pool,
        (c) => qualityRank(c.quality) / maxRank,
        qualityTiebreak,
      ).stream;
    }

    case "bandwidth": {
      const mbps = opts.measuredBandwidthMbps;
      if (!mbps || mbps <= 0) {
        // No measurement on file — guessing a quality/bandwidth match without
        // data isn't meaningfully better than just balancing seeders & size.
        return pickBestStream(streams, "balanced", opts);
      }
      const minutes = opts.estimatedMinutes ?? 90;
      const seconds = minutes * 60;
      // 30% headroom so playback isn't sitting right at the edge of
      // saturating the link (buffering, other devices on the network, etc).
      const budgetBytes = ((mbps * 1_000_000) / 8) * seconds * 0.7;
      const withinBudget = pool.filter(
        (c) => c.sizeBytes > 0 && c.sizeBytes <= budgetBytes,
      );
      const fromPool = withinBudget.length > 0 ? withinBudget : pool;
      const maxRank = Math.max(
        1,
        ...fromPool.map((c) => qualityRank(c.quality)),
      );
      return sortByBoostedDesc(
        fromPool,
        (c) => qualityRank(c.quality) / maxRank,
        qualityTiebreak,
      ).stream;
    }

    case "balanced":
    default: {
      const maxSeeders = Math.max(1, ...pool.map((c) => c.seeders));
      const knownSizes = pool.map((c) => c.sizeBytes).filter((b) => b > 0);
      const maxSize = Math.max(1, ...knownSizes);
      return sortByBoostedDesc(
        pool,
        (c) => {
          const seederScore = c.seeders / maxSeeders;
          // Streams with no parsed size aren't penalized or rewarded — treat
          // as a neutral midpoint rather than guessing.
          const sizeScore = c.sizeBytes > 0 ? 1 - c.sizeBytes / maxSize : 0.5;
          return seederScore * 0.6 + sizeScore * 0.4;
        },
        () => 0,
      ).stream;
    }
  }
}
