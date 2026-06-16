<script lang="ts">
  import type { Stream } from "$lib/types/addons";
  import type { Media, MediaImages, MediaVideos } from "$lib/types/tmdb";
  import Player from "./Player.svelte";
  import { Button } from "$lib/components/ui/button";
  import { ChevronLeft, Star } from "lucide-svelte";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";

  import {
    countryName,
    formatRating,
    formatRuntime,
    getImageOpt,
    getVideoOpt,
    qualityClass,
  } from "$lib/utils";

  import StreamsList from "./StreamsList.svelte";
  import PlayerSimple from "./PlayerSimple.svelte";
  import { api } from "$lib/api";

  let {
    media,
    onsimilar,
  }: {
    media: Media;
    onsimilar?: (m: Media) => void;
    onBack?: () => void;
  } = $props();

  let activeStream: Stream | null = $state(null);
  let activeSubtitles = $state<{ id: string; url: string; lang: string }[]>([]);

  let detailsLoading = $state(false);
  let genres: string[] = $state([]);
  let runtime = $state("");
  let cast: string[] = $state([]);
  let ageRating = $state("");
  let keywords: string[] = $state([]);
  let similar: Media[] = $state([]);
  let originCountry: string[] = $state([]);
  let maxQuality = $state<string | null>();
  let numberOfSeasons = $state<number | null>(null);
  let numberOfEpisodes = $state<number | null>(null);
  let images = $state<MediaImages>();
  let videos = $state<MediaVideos>();

  $effect(() => {
    detailsLoading = true;
    const type = media.media_type;

    Promise.all([
      api.getVideos(media),
      api.getSimilar(media),
      api.getDetails(media),
      api.getImages(media),
    ])
      .then(([vids, similarList, details, img]) => {
        images = img;
        videos = vids;
        similar = similarList;

        genres =
          details.genres?.map((g: { name: string }) => g.name).slice(0, 3) ??
          [];

        runtime = formatRuntime(details);

        cast =
          details.credits?.cast
            ?.slice(0, 5)
            .map((c: { name: string }) => c.name) ?? [];

        ageRating = formatRating(details);

        keywords =
          (type === "movie"
            ? details.keywords?.keywords
            : details.keywords?.results
          )
            ?.slice(0, 4)
            .map((k: { name: string }) => k.name) ?? [];

        originCountry = details.origin_country ?? [];

        if (type === "tv") {
          numberOfSeasons = details.number_of_seasons ?? null;
          numberOfEpisodes = details.number_of_episodes ?? null;
        }

        detailsLoading = false;
      })
      .catch(() => {
        detailsLoading = false;
      });
  });

  function playStream(stream: Stream, season?: number, episode?: number): void {
    activeStream = stream;
    activeSubtitles = [];

    const params = new URLSearchParams({ id: String(media.id), type: media.media_type });
    if (media.media_type === "tv" && season != null && episode != null) {
      params.set("season", String(season));
      params.set("episode", String(episode));
    }

    fetch(`http://localhost:6969/api/subtitles?${params}`)
      .then((r) => r.json())
      .then((subs) => { activeSubtitles = Array.isArray(subs) ? subs : []; })
      .catch(() => {});
  }

  const year = $derived(
    (media.media_type === "tv"
      ? media.first_air_date
      : media.release_date
    )?.slice(0, 4),
  );

  const overviewParagraphs = $derived(
    media.overview
      ?.split(". ")
      .map((s, i, arr) => (i < arr.length - 1 ? s + "." : s)) ?? [],
  );
</script>

{#if activeStream}
  <div class="h-full w-full overflow-hidden rounded-xl bg-black">
    <Player
      src={activeStream.infoHash || activeStream.url}
      {media}
      externalSubtitles={activeSubtitles}
    />
  </div>
{:else}
  <div
    class="relative flex h-full w-full overflow-hidden rounded-xl border border-border bg-background pt-18 pr-6"
  >
    <div
      class="absolute inset-0 scale-110 bg-cover bg-center opacity-30 blur-md"
      style="background-image: url('{getImageOpt(images, 'backdrops', {
        iso: 'en',
        randomize: true,
      })}')"
    ></div>

    <div class="relative z-10 flex h-full w-full gap-1 rounded-2xl">
      <div class="min-w-0 flex-1">
        <ScrollArea class="h-full w-full">
          <div class="p-6 pr-4">
            {#if detailsLoading}
              <div class="flex h-full items-center justify-center">
                <span class="animate-pulse text-sm text-muted-foreground">
                  Loading details...
                </span>
              </div>
            {:else}
              <div class="space-y-4">
                <div class="flex items-center gap-3">
                  <div class="flex items-center gap-2">
                    <div
                      class="flex max-h-20 max-w-64 items-center justify-center"
                    >
                      {#if images && images.logos.length > 0}
                        <img
                          src={getImageOpt(images, "logos", { iso: "en" })}
                          alt="Logo"
                          class="max-h-full w-auto max-w-full object-contain"
                        />
                      {:else}
                        <span class="text-3xl font-bold">{media.title}</span>
                      {/if}
                    </div>
                    {#if year}
                      <Badge variant="default">{year}</Badge>
                    {/if}
                  </div>
                </div>

                <div class="flex flex-wrap items-center gap-3 text-sm">
                  <span class="flex items-center gap-1 text-yellow-400">
                    <Star class="size-4 fill-current" />
                    {media.vote_average?.toFixed(1)}
                  </span>
                  {#if ageRating}
                    <span
                      class="rounded border border-border px-1.5 py-0.5 text-xs"
                    >
                      {ageRating}
                    </span>
                  {/if}
                  {#if originCountry.length}
                    <span
                      class="rounded border border-border px-1.5 py-0.5 text-xs"
                    >
                      {originCountry
                        .map((code) => countryName(code))
                        .join(", ")}
                    </span>
                  {/if}
                  {#if runtime}
                    <span
                      class="rounded border border-border px-1.5 py-0.5 text-xs"
                    >
                      {runtime}
                    </span>
                  {/if}
                  <!-- Seasons + episodes count for TV shows -->
                  {#if media.media_type === "tv" && numberOfSeasons !== null}
                    <span
                      class="rounded border border-border px-1.5 py-0.5 text-xs"
                    >
                      {numberOfSeasons} season{numberOfSeasons !== 1 ? "s" : ""}
                    </span>
                  {/if}
                  {#if media.media_type === "tv" && numberOfEpisodes !== null}
                    <span
                      class="rounded border border-border px-1.5 py-0.5 text-xs"
                    >
                      {numberOfEpisodes} episode{numberOfEpisodes !== 1
                        ? "s"
                        : ""}
                    </span>
                  {/if}
                  {#if maxQuality}
                    <span
                      class="rounded border px-1.5 py-0.5 text-xs font-medium {qualityClass(
                        maxQuality,
                      )}"
                    >
                      {maxQuality.toUpperCase()}
                    </span>
                  {/if}
                </div>

                {#if genres.length}
                  <div class="flex flex-wrap gap-1.5">
                    {#each genres as genre (genre)}
                      <span
                        class="rounded-full bg-secondary px-2.5 py-0.5 text-xs font-medium text-secondary-foreground"
                      >
                        {genre}
                      </span>
                    {/each}
                  </div>
                {/if}

                <Separator />

                {#if videos?.results?.length > 0}
                  <div class="pointer-events-auto space-y-2">
                    <div
                      class="relative aspect-video w-full overflow-hidden rounded-lg border border-border bg-black"
                    >
                      <PlayerSimple
                        src={getVideoOpt(videos, "Trailer", {
                          randomize: true,
                        })}
                        controls={true}
                        bg={media.poster_path}
                      />
                    </div>
                  </div>
                {/if}

                <div class="space-y-2">
                  <h3 class="text-sm font-semibold">Overview</h3>
                  <div class="space-y-2 text-sm text-muted-foreground">
                    {#each overviewParagraphs as paragraph, i (i)}
                      <p>{paragraph}</p>
                    {/each}
                  </div>
                </div>

                {#if cast.length}
                  <div>
                    <h3 class="mb-2 text-sm font-semibold">Cast</h3>
                    <div class="flex flex-wrap gap-1.5">
                      {#each cast as person (person)}
                        <span
                          class="rounded-full bg-secondary px-2.5 py-0.5 text-xs text-secondary-foreground"
                        >
                          {person}
                        </span>
                      {/each}
                    </div>
                  </div>
                {/if}

                {#if keywords.length}
                  <div>
                    <h3 class="mb-2 text-sm font-semibold">
                      This {media.media_type === "tv" ? "show" : "film"} is
                    </h3>
                    <div class="flex flex-wrap gap-1.5">
                      {#each keywords as keyword (keyword)}
                        <span
                          class="rounded-full bg-secondary px-2.5 py-0.5 text-xs text-secondary-foreground"
                        >
                          {keyword}
                        </span>
                      {/each}
                    </div>
                  </div>
                {/if}

                {#if similar.length}
                  <div class="space-y-2">
                    <h3 class="text-sm font-semibold">More like this</h3>
                    <div class="grid grid-cols-3 gap-3 sm:grid-cols-4">
                      {#each similar as item (item.id)}
                        <div
                          role="button"
                          tabindex="0"
                          class="cursor-pointer overflow-hidden rounded-md transition-opacity hover:opacity-75"
                          onclick={() => onsimilar?.(item)}
                          onkeydown={(e) =>
                            e.key === "Enter" && onsimilar?.(item)}
                        >
                          <img
                            src={item.poster_path}
                            alt={item.media_type === "tv"
                              ? item.name
                              : item.title}
                            class="aspect-2/3 w-full object-cover"
                          />
                        </div>
                      {/each}
                    </div>
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        </ScrollArea>
      </div>
      <div
        class="flex h-full w-[35%] min-w-0 flex-none flex-col overflow-hidden"
      >
        <StreamsList
          {media}
          onPlayStream={(s: Stream) => playStream(s)}
          bind:maxQuality
        />
      </div>
    </div>
  </div>
{/if}
