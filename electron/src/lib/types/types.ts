import { Media } from "$lib/types/tmdb";

export type Page =
  | { type: "home" }
  | { type: "myList" }
  | { type: "query"; query: string }
  | { type: "mediaView"; media: Media };
