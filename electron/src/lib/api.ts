import type { Details, Media, MediaImages } from "$lib/types/tmdb";
import type { Stream } from "$lib/types/addons";

const BASE = "http://localhost:6969/api";

export const api = {
  search: (q: string): Promise<Media[]> =>
    fetch(`${BASE}/search?q=${encodeURIComponent(q)}`).then((r) => r.json()),
  getKeywords: (q: string): Promise<{ id: number; name: string }[]> =>
    fetch(`${BASE}/keywords?q=${encodeURIComponent(q)}`).then((r) => r.json()),
  getStreams: (tmdbId: number): Promise<Stream[]> =>
    fetch(`${BASE}/streams?id=${tmdbId}`).then((r) => r.json()),
  getClips: async (media: Media): Promise<string[]> => {
    const r = await fetch(
      `${BASE}/clips?id=${media.id}&type=${media.media_type}`,
    );
    const d = await r.json();
    let urls: string[] = [];
    if (Array.isArray(d.urls)) {
      urls = d.urls
        .map((item: unknown) => {
          if (
            typeof item === "object" &&
            item !== null &&
            "url" in item &&
            typeof item.url === "string"
          ) {
            return item.url;
          }
          if (typeof item === "string") return item;
          return null;
        })
        .filter(
          (url: string): url is string => url !== null && url.trim() !== "",
        );
    }
    return urls;
  },
  getTrailer: async (media: Media): Promise<string> => {
    const r = await fetch(
      `${BASE}/trailer?id=${media.id}&type=${media.media_type}`,
    );
    const d = await r.json();
    let url: string | null = null;
    if (typeof d === "string" && d.trim() !== "") {
      url = d;
    } else if (
      d &&
      typeof d === "object" &&
      typeof d.url === "string" &&
      d.url.trim() !== ""
    ) {
      url = d.url;
    }
    return url;
  },
  getSimilar: async (media: Media): Promise<Media[]> => {
    return fetch(`${BASE}/similar?id=${media.id}&type=${media.media_type}`)
      .then((r) => r.json())
      .then((d: Media[]) => {
        return d;
      });
  },
  getDetails: async (media: Media): Promise<Details> =>
    fetch(
      `http://localhost:6969/api/details?id=${media.id}&type=${media.media_type}`,
    ).then((r) => r.json()),
  getImages: async (media: Media): Promise<MediaImages> =>
    fetch(
      `${BASE}/images?id=${media.id}&type=${media.media_type}`,
    ).then((r) => r.json()),
};
