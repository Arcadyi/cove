import { Media } from "$lib/types/tmdb";

export type Page =
  | { type: "home" }
  | { type: "myList" }
  | { type: "explore" }
  | { type: "settings" }
  | { type: "insights" }
  | { type: "query"; query: string }
  | { type: "mediaView"; media: Media };


export interface UpcomingItem {
  tmdbId: number;
  title: string;
  posterPath: string;
  season: number;
  episode: number;
  episodeName: string;
  airDate: string; // YYYY-MM-DD from TMDB
  stillPath: string;
}
