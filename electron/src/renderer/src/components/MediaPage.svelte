<script lang="ts">
  import type { Stream } from "$lib/types/addons";
  import type { Media } from "$lib/types/tmdb";
  import Player from "./Player.svelte";
  import { Button } from "$lib/components/ui/button";
  import { ChevronLeft, Star } from "lucide-svelte";
  import { Badge } from "$lib/components/ui/badge/index.js";
  import { Separator } from "$lib/components/ui/separator/index.js";
  import { ScrollArea } from "$lib/components/ui/scroll-area/index.js";

  import {countryName, qualityClass} from "$lib/utils";
  import StreamsList from "./StreamsList.svelte";

  let {
    media,
    onsimilar,
    onBack,
  }: {
    media: Media;
    onsimilar?: (m: Media) => void;
    onBack?: () => void;
  } = $props();

  let activeStream: Stream | null = $state(null);

  let detailsLoading = $state(false);
  let genres: string[] = $state([]);
  let runtime = $state("");
  let cast: string[] = $state([]);
  let ageRating = $state("");
  let keywords: string[] = $state([]);
  let trailer: string | null = $state(null);
  let similar: Media[] = $state([]);
  let originCountry: string[] = $state([]);
  let maxQuality = $state<string | null>();

  $effect(() => {
    detailsLoading = true;
    const type = media.media_type;

    Promise.all([
      fetch(`http://localhost:6969/api/trailer?id=${media.id}&type=${type}`)
        .then((r) => r.json())
        .then((d) => d.url),
      fetch(`http://localhost:6969/api/similar?id=${media.id}&type=${type}`)
        .then((r) => r.json())
        .then((d) => d ?? []),
      fetch(`http://localhost:6969/api/details?id=${media.id}&type=${type}`)
        .then((r) => r.json())
        .then((d) => d),
    ])
      .then(([trailerUrl, similarList, details]) => {
        trailer = trailerUrl;
        similar = similarList;

        genres =
          details.genres?.map((g: { name: string }) => g.name).slice(0, 3) ??
          [];

        runtime =
          details.runtime > 0
            ? `${Math.floor(details.runtime / 60)}h ${details.runtime % 60}m`
            : details.episode_run_time?.[0]
              ? `${details.episode_run_time[0]}m / ep`
              : "";

        cast =
          details.credits?.cast
            ?.slice(0, 5)
            .map((c: { name: string }) => c.name) ?? [];

        ageRating = (() => {
          for (const r of details.release_dates?.results ?? []) {
            if (r.iso_3166_1 === "US") {
              for (const rd of r.release_dates ?? []) {
                if (rd.certification) return rd.certification;
              }
            }
          }

          for (const r of details.content_ratings?.results ?? []) {
            if (r.iso_3166_1 === "US" && r.rating) return r.rating;
          }

          return "";
        })();

        keywords =
          (type === "movie"
            ? details.keywords?.keywords
            : details.keywords?.results
          )
            ?.slice(0, 4)
            .map((k: { name: string }) => k.name) ?? [];

        originCountry = details.origin_country ?? [];

        detailsLoading = false;
      })
      .catch(() => {
        detailsLoading = false;
      });
  });

  function playStream(stream: Stream): void {
    activeStream = stream;
  }

  const title = $derived(media.media_type === "tv" ? media.name : media.title);
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

  const trailerUrl = $derived(
    trailer
      ? `${trailer}?autoplay=1&controls=0&modestbranding=1&loop=1&rel=0&iv_load_policy=3&disablekb=1`
      : null,
  );
</script>

{#if activeStream}
  <div class="relative h-full w-full overflow-hidden rounded-xl bg-black">
    <Player src={activeStream.infoHash || activeStream.url} {media} />
    <Button
      variant="outline"
      size="sm"
      class="absolute top-6 left-6 z-50 bg-background/50 backdrop-blur-md"
      onclick={() => (activeStream = null)}
    >
      <ChevronLeft class="mr-1 size-4" /> Back
    </Button>
  </div>
{:else}
  <div
    class="relative flex h-full w-full overflow-hidden rounded-xl border border-border bg-background"
  >
    <div
      class="absolute inset-0 scale-110 bg-cover bg-center opacity-30 blur-sm"
      style="background-image: url('{media.poster_path}')"
    ></div>

    <div class="relative z-10 flex h-full w-full justify-stretch">
      <ScrollArea class="h-full w-[65%]">
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
                {#if onBack}
                  <Button
                    variant="outline"
                    size="icon-lg"
                    onclick={onBack}
                    title="Go back"
                  >
                    <ChevronLeft />
                  </Button>
                {/if}

                <div class="flex flex-wrap items-center gap-2">
                  <h2
                    class="text-3xl font-bold tracking-tight text-foreground drop-shadow-lg"
                  >
                    {title}
                  </h2>
                  {#if year}
                    <Badge variant="default">{year}</Badge>
                  {/if}
                </div>
              </div>

              <div class="flex items-center gap-3 text-sm">
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
                    {originCountry.map((code) => countryName(code)).join(", ")}
                  </span>
                {/if}
                {#if runtime}
                  <span
                    class="rounded border border-border px-1.5 py-0.5 text-xs"
                  >
                    {runtime}</span
                  >
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

              {#if trailerUrl}
                <div class="space-y-2">
                  <div
                    class="relative aspect-video w-full overflow-hidden rounded-lg border border-border"
                  >
                    <iframe
                      src={trailerUrl}
                      title={`${title} trailer`}
                      class="absolute inset-0 h-full w-full"
                      allow="autoplay; encrypted-media"
                    ></iframe>
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
      <StreamsList
        {media}
        onPlayStream={(s: Stream) => playStream(s)}
        bind:maxQuality
      />
    </div>
  </div>
{/if}
